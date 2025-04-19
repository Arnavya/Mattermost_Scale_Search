// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package elasticsearch

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/shared/mlog"
	"github.com/mattermost/mattermost/server/public/shared/request"
)

func TestIndexPost(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping elasticsearch test in short mode")
	}

	// Create test config
	config := &model.Config{}
	config.SetDefaults()
	*config.ElasticsearchSettings.EnableIndexing = true
	*config.ElasticsearchSettings.ConnectionURL = "http://localhost:9200" // Adjust for your test environment
	*config.ElasticsearchSettings.Username = "elastic"
	*config.ElasticsearchSettings.Password = "changeme"
	*config.ElasticsearchSettings.SnippetThreshold = 10

	// Create test logger
	testLogger := mlog.CreateConsoleTestLogger(t)
	defer testLogger.Shutdown()

	// Create Elasticsearch client
	client, err := NewClient(config, testLogger)
	require.NoError(t, err, "Failed to create Elasticsearch client")
	require.True(t, client.IsReady(), "Elasticsearch client should be ready")

	// Clear test data
	ctx := request.TestContext(t)
	_ = ctx

	// Test creating index
	err = client.CreateMessageIndex()
	require.NoError(t, err, "Failed to create index")

	// Test indexing a post
	testTeamId := model.NewId()
	testPost := &model.Post{
		Id:        model.NewId(),
		CreateAt:  model.GetMillis(),
		UpdateAt:  model.GetMillis(),
		UserId:    model.NewId(),
		ChannelId: model.NewId(),
		Message:   "This is a test message for Elasticsearch integration test",
		Type:      model.PostTypeDefault,
	}

	err = client.IndexPost(testPost, testTeamId)
	require.NoError(t, err, "Failed to index post")

	// Force refresh to make the post searchable immediately
	err = client.RefreshIndex()
	require.NoError(t, err, "Failed to refresh index")

	// Give Elasticsearch time to process
	time.Sleep(1 * time.Second)

	// Test searching for the post
	channels := model.ChannelList{
		&model.Channel{
			Id: testPost.ChannelId,
		},
	}
	searchParams := []*model.SearchParams{
		{
			Terms:      "test message",
			IsHashtag:  false,
			InChannels: []string{testPost.ChannelId},
		},
	}

	postIds, matches, err := client.SearchPosts(channels, searchParams, 0, 10)
	require.NoError(t, err, "Failed to search posts")
	assert.Len(t, postIds, 1, "Expected 1 post in search results")
	assert.Equal(t, testPost.Id, postIds[0], "Post ID in search results should match test post ID")

	// Test deleting the post
	err = client.DeletePost(testPost.Id)
	require.NoError(t, err, "Failed to delete post")

	// Force refresh again
	err = client.RefreshIndex()
	require.NoError(t, err, "Failed to refresh index")

	// Give Elasticsearch time to process
	time.Sleep(1 * time.Second)

	// Test that the post is no longer in search results
	postIds, matches, err = client.SearchPosts(channels, searchParams, 0, 10)
	require.NoError(t, err, "Failed to search posts")
	assert.Len(t, postIds, 0, "Expected 0 posts in search results after deletion")
}

func TestBatchIndexPosts(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping elasticsearch test in short mode")
	}

	// Create test config
	config := &model.Config{}
	config.SetDefaults()
	*config.ElasticsearchSettings.EnableIndexing = true
	*config.ElasticsearchSettings.ConnectionURL = "http://localhost:9200" // Adjust for your test environment
	*config.ElasticsearchSettings.Username = "elastic"
	*config.ElasticsearchSettings.Password = "changeme"

	// Create test logger
	testLogger := mlog.CreateConsoleTestLogger(t)
	defer testLogger.Shutdown()

	// Create Elasticsearch client
	client, err := NewClient(config, testLogger)
	require.NoError(t, err, "Failed to create Elasticsearch client")
	require.True(t, client.IsReady(), "Elasticsearch client should be ready")

	// Clear test data
	ctx := request.TestContext(t)
	_ = ctx

	// Test creating index
	err = client.CreateMessageIndex()
	require.NoError(t, err, "Failed to create index")

	// Test batch indexing posts
	testTeamId := model.NewId()
	testChannelId := model.NewId()
	numPosts := 10
	testPosts := make([]*model.Post, numPosts)
	
	for i := 0; i < numPosts; i++ {
		testPosts[i] = &model.Post{
			Id:        model.NewId(),
			CreateAt:  model.GetMillis(),
			UpdateAt:  model.GetMillis(),
			UserId:    model.NewId(),
			ChannelId: testChannelId,
			Message:   "Batch test message " + model.NewId(),
			Type:      model.PostTypeDefault,
		}
	}

	// Test batch indexing
	err = client.BatchIndexPosts(testPosts, testTeamId)
	require.NoError(t, err, "Failed to batch index posts")

	// Force refresh to make the posts searchable immediately
	err = client.RefreshIndex()
	require.NoError(t, err, "Failed to refresh index")

	// Give Elasticsearch time to process
	time.Sleep(1 * time.Second)

	// Test searching for the posts
	channels := model.ChannelList{
		&model.Channel{
			Id: testChannelId,
		},
	}
	searchParams := []*model.SearchParams{
		{
			Terms:      "Batch test message",
			IsHashtag:  false,
			InChannels: []string{testChannelId},
		},
	}

	postIds, matches, err := client.SearchPosts(channels, searchParams, 0, 20)
	require.NoError(t, err, "Failed to search posts")
	assert.Len(t, postIds, numPosts, "Expected %d posts in search results", numPosts)

	// Clean up
	for _, post := range testPosts {
		err = client.DeletePost(post.Id)
		require.NoError(t, err, "Failed to delete post")
	}
} 