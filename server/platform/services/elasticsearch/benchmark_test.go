// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package elasticsearch

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/shared/mlog"
	"github.com/mattermost/mattermost/server/v8/channels/app"
	"github.com/mattermost/mattermost/server/v8/channels/store/sqlstore"
	"github.com/mattermost/mattermost/server/v8/channels/testlib"
)

func BenchmarkSearchPerformance(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark test in short mode")
	}

	// Create a test server with Elasticsearch enabled
	th := testlib.Setup(b).InitBasic()
	defer th.TearDown()

	// Create test logger
	testLogger := mlog.CreateConsoleTestLogger(b)
	defer testLogger.Shutdown()

	// Create a test channel
	channel, appErr := th.App.CreateChannel(th.Context, &model.Channel{
		DisplayName: "Test Channel",
		Name:        "test-channel",
		Type:        model.ChannelTypeOpen,
		TeamId:      th.BasicTeam.Id,
		CreatorId:   th.BasicUser.Id,
	}, false)
	require.Nil(b, appErr)

	// Add second user to channel
	_, appErr = th.App.AddChannelMember(th.Context, th.BasicUser2.Id, channel, app.ChannelMemberOpts{})
	require.Nil(b, appErr)

	// Generate test posts
	numPosts := 10000
	searchTerms := []string{
		"unique_search_term_alpha",
		"unique_search_term_beta",
		"unique_search_term_gamma",
		"unique_search_term_delta",
	}

	b.Logf("Generating %d test posts...", numPosts)
	testPosts := make([]*model.Post, numPosts)
	for i := 0; i < numPosts; i++ {
		// Add search term to approximately 1% of posts to make it a bit challenging to find
		message := fmt.Sprintf("This is test post %d", i)
		if i%100 == 0 {
			searchTermIndex := (i / 100) % len(searchTerms)
			message = fmt.Sprintf("%s with %s", message, searchTerms[searchTermIndex])
		}

		testPosts[i] = &model.Post{
			Id:        model.NewId(),
			CreateAt:  model.GetMillis(),
			UpdateAt:  model.GetMillis(),
			UserId:    th.BasicUser.Id,
			ChannelId: channel.Id,
			Message:   message,
		}
	}

	// Insert posts into the database
	sqlStore := sqlstore.New(th.App.Config().SqlSettings, nil)
	for _, post := range testPosts {
		_, err := sqlStore.Post().Save(th.Context, post)
		require.NoError(b, err)
	}

	// Set up Elasticsearch
	config := th.App.Config().Clone()
	*config.ElasticsearchSettings.EnableIndexing = true
	*config.ElasticsearchSettings.EnableSearching = true
	*config.ElasticsearchSettings.ConnectionURL = "http://localhost:9200"
	*config.ElasticsearchSettings.Username = "elastic"
	*config.ElasticsearchSettings.Password = "changeme"
	
	// Create Elasticsearch service
	esService, err := New(config, testLogger)
	require.NoError(b, err)
	require.NotNil(b, esService)

	// Create a search adapter
	searchAdapter := NewSearchAdapter(esService)
	
	// Connect to Elasticsearch and index posts
	if !esService.IsActive() {
		b.Skip("Elasticsearch is not active. Skipping benchmark test.")
	}

	// Ensure the index exists
	err = esService.client.CreateMessageIndex()
	require.NoError(b, err)

	// Index all posts
	b.Logf("Indexing %d posts in Elasticsearch...", numPosts)
	for i, post := range testPosts {
		err := esService.IndexPost(post, th.BasicTeam.Id)
		require.NoError(b, err)
		
		if i%1000 == 0 && i > 0 {
			b.Logf("Indexed %d posts...", i)
		}
	}

	// Force refresh to make the posts searchable immediately
	err = esService.RefreshIndex()
	require.NoError(b, err)
	
	// Give Elasticsearch time to process
	time.Sleep(2 * time.Second)
	
	// Create search params for the benchmark tests
	channelList := model.ChannelList{channel}
	searchParamsList := []*model.SearchParams{
		{
			Terms:      searchTerms[0],
			InChannels: []string{channel.Id},
		},
	}

	// Benchmark SQL search
	b.Run("SQLSearch", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := sqlStore.Post().SearchPostsInTeamForUser(th.Context, searchParamsList, th.BasicUser.Id, th.BasicTeam.Id, 0, 20)
			require.NoError(b, err)
		}
	})

	// Benchmark Elasticsearch search
	b.Run("ElasticsearchSearch", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _, err := searchAdapter.SearchPosts(channelList, searchParamsList, 0, 20)
			require.NoError(b, err)
		}
	})
} 