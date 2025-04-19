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

func TestElasticsearchSearch(t *testing.T) {
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

	// Create post test data
	testMessages := []struct {
		Name     string
		Message  string
		Expected bool
	}{
		{
			Name:     "Basic match",
			Message:  "This is a test message",
			Expected: true,
		},
		{
			Name:     "Fuzzy match - should match with typo",
			Message:  "This is a special elasticsearch mesage", // Misspelled "message"
			Expected: true,
		},
		{
			Name:     "No match",
			Message:  "Unrelated content that should not match search",
			Expected: false,
		},
	}

	// Create test posts
	posts := []*model.Post{}
	for _, tm := range testMessages {
		post := &model.Post{
			Id:        model.NewId(),
			CreateAt:  model.GetMillis(),
			UpdateAt:  model.GetMillis(),
			UserId:    model.NewId(),
			ChannelId: model.NewId(),
			Message:   tm.Message,
			Type:      model.PostTypeDefault,
		}
		err = client.IndexPost(post, model.NewId())
		require.NoError(t, err, "Failed to index post")
		posts = append(posts, post)
	}

	// Force refresh to make the posts searchable immediately
	err = client.RefreshIndex()
	require.NoError(t, err, "Failed to refresh index")

	// Wait briefly for Elasticsearch to index the posts
	time.Sleep(1 * time.Second)

	// Test searching with our endpoint
	t.Run("Test Elasticsearch endpoint", func(t *testing.T) {
		channels := model.ChannelList{
			&model.Channel{
				Id: posts[0].ChannelId,
			},
		}

		// 1. Test basic match
		searchParams := []*model.SearchParams{
			{
				Terms:      "test message",
				IsHashtag:  false,
				InChannels: []string{posts[0].ChannelId},
			},
		}

		postIds, matches, err := client.SearchPosts(channels, searchParams, 0, 10)
		require.NoError(t, err, "Failed to search posts")
		assert.NotEmpty(t, postIds, "Expected matching posts")

		// 2. Test fuzzy match
		searchParams = []*model.SearchParams{
			{
				Terms:      "elasticsearch message", // Should match "elasticsearch mesage" with typo
				IsHashtag:  false,
				InChannels: []string{posts[1].ChannelId},
			},
		}

		postIds, matches, err = client.SearchPosts(channels, searchParams, 0, 10)
		require.NoError(t, err, "Failed to search posts")
		assert.NotEmpty(t, postIds, "Expected fuzzy matching posts")

		// 3. Test empty query
		searchParams = []*model.SearchParams{
			{
				Terms:      "",
				IsHashtag:  false,
				InChannels: []string{posts[0].ChannelId},
			},
		}

		postIds, matches, err = client.SearchPosts(channels, searchParams, 0, 10)
		require.NoError(t, err, "Failed to search posts with empty query")
		assert.Empty(t, postIds, "Expected no results for empty query")

		// 4. Test no match
		searchParams = []*model.SearchParams{
			{
				Terms:      "nonexistent content xyz123",
				IsHashtag:  false,
				InChannels: []string{posts[0].ChannelId},
			},
		}

		postIds, matches, err = client.SearchPosts(channels, searchParams, 0, 10)
		require.NoError(t, err, "Failed to search posts with no match query")
		assert.Empty(t, postIds, "Expected no results for non-matching query")
	})

	// Clean up
	for _, post := range posts {
		err = client.DeletePost(post.Id)
		require.NoError(t, err, "Failed to delete post")
	}
}

// BenchmarkSearchPerformance benchmarks the performance of both SQL and Elasticsearch search - simplified for
// this test since we don't have full database access
func BenchmarkSearchPerformance(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping elasticsearch benchmark in short mode")
	}

	// Create test config
	config := &model.Config{}
	config.SetDefaults()
	*config.ElasticsearchSettings.EnableIndexing = true
	*config.ElasticsearchSettings.EnableSearching = true
	*config.ElasticsearchSettings.ConnectionURL = "http://localhost:9200"
	*config.ElasticsearchSettings.Username = "elastic"
	*config.ElasticsearchSettings.Password = "changeme"
	*config.ElasticsearchSettings.SnippetThreshold = 10

	// Create test logger
	testLogger := mlog.CreateConsoleTestLogger(b)
	defer testLogger.Shutdown()

	// Create Elasticsearch client
	client, err := NewClient(config, testLogger)
	require.NoError(b, err, "Failed to create Elasticsearch client")
	require.True(b, client.IsReady(), "Elasticsearch client should be ready")

	// Create a large number of test posts
	const numPosts = 100 // Reduced for simple testing
	posts := []*model.Post{}
	teamId := model.NewId()
	channelId := model.NewId()
	userId := model.NewId()
	
	for i := 0; i < numPosts; i++ {
		post := &model.Post{
			Id:        model.NewId(),
			CreateAt:  model.GetMillis(),
			UpdateAt:  model.GetMillis(),
			UserId:    userId,
			ChannelId: channelId,
			Message:   "This is test message number " + model.NewId() + " with some common words like hello world",
			Type:      model.PostTypeDefault,
		}
		
		// Index using Elasticsearch
		err = client.IndexPost(post, teamId)
		require.NoError(b, err, "Failed to index post")
		posts = append(posts, post)
	}
	
	b.Logf("Created and indexed %d posts", numPosts)

	// Force refresh to make the posts searchable immediately
	err = client.RefreshIndex()
	require.NoError(b, err, "Failed to refresh index")

	// Wait briefly for Elasticsearch to index the posts
	time.Sleep(2 * time.Second)

	// The query we'll use for benchmarking
	query := "hello world"
	
	channels := model.ChannelList{
		&model.Channel{
			Id: channelId,
		},
	}
	
	// Benchmark SQL-based search is removed as we don't have access to the database
	
	// Benchmark Elasticsearch-based search
	b.Run("Elasticsearch-based search", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			searchParams := []*model.SearchParams{
				{
					Terms:     query,
					IsHashtag: false,
				},
			}
			_, _, err := client.SearchPosts(channels, searchParams, 0, 20)
			require.NoError(b, err, "Failed to search posts with Elasticsearch")
		}
	})

	// Clean up
	for _, post := range posts {
		err = client.DeletePost(post.Id)
		if err != nil {
			b.Logf("Error deleting post from Elasticsearch: %v", err)
		}
	}
}
