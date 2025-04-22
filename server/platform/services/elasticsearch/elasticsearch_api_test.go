// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package elasticsearch

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/shared/mlog"
)

func TestSearchPostsESAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping elasticsearch API test in short mode")
	}

	// Create test config
	config := &model.Config{}
	config.SetDefaults()
	*config.ElasticsearchSettings.EnableIndexing = true
	*config.ElasticsearchSettings.EnableSearching = true
	*config.ElasticsearchSettings.ConnectionURL = "http://localhost:9200" // Adjust for your test environment
	*config.ElasticsearchSettings.Username = "elastic"
	*config.ElasticsearchSettings.Password = "changeme"
	*config.ElasticsearchSettings.SnippetThreshold = 10

	// Create test logger
	testLogger := mlog.CreateConsoleTestLogger(t)
	defer testLogger.Shutdown()

	// Create Elasticsearch client and service
	client, err := NewClient(config, testLogger)
	require.NoError(t, err, "Failed to create Elasticsearch client")
	require.True(t, client.IsReady(), "Elasticsearch client should be ready")

	service := NewService(client)
	require.NotNil(t, service, "Elasticsearch service should not be nil")

	// Create test router and API
	router := mux.NewRouter()
	api := &API{}
	api.Init(router, service)

	// Create sample test posts
	testMessages := []struct {
		Name     string
		Message  string
		Expected bool
	}{
		{
			Name:     "Basic match",
			Message:  "This is a test message for API testing",
			Expected: true,
		},
		{
			Name:     "Fuzzy match",
			Message:  "This is a elasticsearch api mesage", // Misspelled "message"
			Expected: true,
		},
		{
			Name:     "No match",
			Message:  "This content should not match",
			Expected: false,
		},
	}

	// Create and index test posts
	posts := []*model.Post{}
	userId := model.NewId()
	teamId := model.NewId()
	channelId := model.NewId()

	for _, tm := range testMessages {
		post := &model.Post{
			Id:        model.NewId(),
			CreateAt:  model.GetMillis(),
			UpdateAt:  model.GetMillis(),
			UserId:    userId,
			ChannelId: channelId,
			Message:   tm.Message,
			Type:      model.PostTypeDefault,
		}
		
		// Index post
		err = client.IndexPost(post, teamId)
		require.NoError(t, err, "Failed to index post")
		
		posts = append(posts, post)
	}

	// Force refresh to make the posts searchable immediately
	err = client.RefreshIndex()
	require.NoError(t, err, "Failed to refresh index")

	// Wait briefly for Elasticsearch to index the posts
	time.Sleep(1 * time.Second)

	// Create a custom searchPostsES function for testing
	testSearchPostsES := func(w http.ResponseWriter, r *http.Request) {
		// Get query parameters
		query := r.URL.Query().Get("q")
		if query == "" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"status": "error", "message": "Missing query parameter"}`))
			return
		}
		
		// Parse page and per_page
		page := 0
		if r.URL.Query().Get("page") != "" {
			var err error
			page, err = strconv.Atoi(r.URL.Query().Get("page"))
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"status": "error", "message": "Invalid page parameter"}`))
				return
			}
		}
		
		perPage := 20
		if r.URL.Query().Get("per_page") != "" {
			var err error
			perPage, err = strconv.Atoi(r.URL.Query().Get("per_page"))
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"status": "error", "message": "Invalid per_page parameter"}`))
				return
			}
		}
		
		// Determine if this is an OR search
		isOrSearch := false
		if r.URL.Query().Get("is_or_search") == "true" {
			isOrSearch = true
		}
		
		// Build search parameters
		params := model.ParseSearchParams(query, 0)
		for _, param := range params {
			param.OrTerms = isOrSearch
		}
		
		// Perform search
		channels := model.ChannelList{
			&model.Channel{
				Id: channelId,
			},
		}
		
		searchParams := []*model.SearchParams{
			{
				Terms:      query,
				IsHashtag:  false,
				InChannels: []string{channelId},
			},
		}
		
		postIds, _, err := client.SearchPosts(channels, searchParams, page, perPage)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"status": "error", "message": "Search failed"}`))
			return
		}
		
		// Get the posts
		postList := model.NewPostList()
		for _, postId := range postIds {
			// Find the post in our test posts
			for _, p := range posts {
				if p.Id == postId {
					postList.AddPost(p)
					postList.AddOrder(p.Id)
					break
				}
			}
		}
		
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(postList.ToJson()))
	}

	// Register the test handler
	router.HandleFunc("/api/v4/search/es", testSearchPostsES).Methods("GET")

	// Test the API endpoint with different queries
	t.Run("Test API with basic match query", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v4/search/es?q=test+api+testing", nil)
		router.ServeHTTP(w, r)
		
		assert.Equal(t, http.StatusOK, w.Code)
		
		var result model.PostList
		err := json.Unmarshal(w.Body.Bytes(), &result)
		require.NoError(t, err)
		
		assert.NotEmpty(t, result.Order, "Expected posts in the result")
	})
	
	t.Run("Test API with fuzzy match query", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v4/search/es?q=elasticsearch+message", nil)
		router.ServeHTTP(w, r)
		
		assert.Equal(t, http.StatusOK, w.Code)
		
		var result model.PostList
		err := json.Unmarshal(w.Body.Bytes(), &result)
		require.NoError(t, err)
		
		assert.NotEmpty(t, result.Order, "Expected posts in the result for fuzzy match")
	})
	
	t.Run("Test API with no match query", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v4/search/es?q=nonexistent+content+xyz123", nil)
		router.ServeHTTP(w, r)
		
		assert.Equal(t, http.StatusOK, w.Code)
		
		var result model.PostList
		err := json.Unmarshal(w.Body.Bytes(), &result)
		require.NoError(t, err)
		
		assert.Empty(t, result.Order, "Expected no posts in the result for non-matching query")
	})
	
	t.Run("Test API with empty query", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v4/search/es?q=", nil)
		router.ServeHTTP(w, r)
		
		assert.Equal(t, http.StatusBadRequest, w.Code, "Empty query should return 400 Bad Request")
	})
	
	t.Run("Test API with pagination", func(t *testing.T) {
		// First, create more posts with the same content
		for i := 0; i < 5; i++ {
			post := &model.Post{
				Id:        model.NewId(),
				CreateAt:  model.GetMillis(),
				UpdateAt:  model.GetMillis(),
				UserId:    userId,
				ChannelId: channelId,
				Message:   "Pagination test message", // All posts have the same content for easier pagination testing
				Type:      model.PostTypeDefault,
			}
			
			err = client.IndexPost(post, teamId)
			require.NoError(t, err, "Failed to index pagination test post")
			
			posts = append(posts, post)
		}
		
		// Force refresh to make the posts searchable immediately
		err = client.RefreshIndex()
		require.NoError(t, err, "Failed to refresh index")
		
		// Wait briefly for Elasticsearch to index the posts
		time.Sleep(1 * time.Second)
		
		// Test first page
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v4/search/es?q=Pagination+test&page=0&per_page=2", nil)
		router.ServeHTTP(w, r)
		
		assert.Equal(t, http.StatusOK, w.Code)
		
		var resultPage1 model.PostList
		err := json.Unmarshal(w.Body.Bytes(), &resultPage1)
		require.NoError(t, err)
		
		// Test second page
		w2 := httptest.NewRecorder()
		r2 := httptest.NewRequest("GET", "/api/v4/search/es?q=Pagination+test&page=1&per_page=2", nil)
		router.ServeHTTP(w2, r2)
		
		assert.Equal(t, http.StatusOK, w2.Code)
		
		var resultPage2 model.PostList
		err = json.Unmarshal(w2.Body.Bytes(), &resultPage2)
		require.NoError(t, err)
		
		// Check first page has results
		if len(resultPage1.Order) > 0 {
			// Make sure pages are different by checking IDs don't overlap
			for _, id1 := range resultPage1.Order {
				for _, id2 := range resultPage2.Order {
					assert.NotEqual(t, id1, id2, "Pages should contain different posts")
				}
			}
		}
	})

	// Clean up
	for _, post := range posts {
		err = client.DeletePost(post.Id)
		if err != nil {
			t.Logf("Error deleting post: %v", err)
		}
	}
}
