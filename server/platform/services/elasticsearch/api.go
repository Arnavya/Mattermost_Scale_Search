// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package elasticsearch

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/shared/mlog"
	"github.com/mattermost/mattermost/server/v8/channels/audit"
	"github.com/mattermost/mattermost/server/v8/channels/web"
)

// API contains handlers for Elasticsearch API endpoints
type API struct {
	service *Service
	root    *mux.Router
}

// Init initializes the API routes
func (api *API) Init(root *mux.Router, service *Service) {
	api.service = service
	api.root = root

	// Register routes
	esRouter := root.PathPrefix("/api/v4/search/es").Subrouter()
	esRouter.Handle("", api.ApiSessionRequired(searchPostsES)).Methods("GET")
	
	// Batch indexing API
	adminRouter := root.PathPrefix("/api/v4/elasticsearch").Subrouter()
	adminRouter.Handle("/index_batch", api.ApiSessionRequired(api.ApiRequireSystemAdmin(indexPostBatch))).Methods("POST")
	adminRouter.Handle("/test_config", api.ApiSessionRequired(api.ApiRequireSystemAdmin(testElasticsearchConfig))).Methods("POST")
}

// ApiHandler wraps an http.Handler with authentication for APIs
func (api *API) ApiHandler(h func(*web.Context, http.ResponseWriter, *http.Request)) http.Handler {
	return &web.Handler{
		HandleFunc:     h,
		RequireSession: false,
		TrustRequester: false,
		RequireMfa:     false,
		IsStatic:       false,
	}
}

// ApiSessionRequired wraps an http.Handler with authentication for APIs that require a session
func (api *API) ApiSessionRequired(h func(*web.Context, http.ResponseWriter, *http.Request)) http.Handler {
	return &web.Handler{
		HandleFunc:     h,
		RequireSession: true,
		TrustRequester: false,
		RequireMfa:     true,
		IsStatic:       false,
	}
}

// ApiRequireSystemAdmin wraps an http.Handler to require a system admin
func (api *API) ApiRequireSystemAdmin(h func(*web.Context, http.ResponseWriter, *http.Request)) func(*web.Context, http.ResponseWriter, *http.Request) {
	return func(c *web.Context, w http.ResponseWriter, r *http.Request) {
		if !c.App.SessionHasPermissionTo(*c.AppContext.Session(), model.PermissionManageSystem) {
			c.Err = model.NewAppError("ApiRequireSystemAdmin", "api.context.system_permissions_required.app_error", nil, "", http.StatusForbidden)
			return
		}
		h(c, w, r)
	}
}

// searchPostsES is the handler for searching posts using Elasticsearch
func searchPostsES(c *web.Context, w http.ResponseWriter, r *http.Request) {
	// Check if service is active
	esService := c.App.Srv().ElasticsearchService()
	if esService == nil || !esService.IsActive() {
		c.Err = model.NewAppError("searchPostsES", "api.elasticsearch.search_posts.service_disabled", nil, "", http.StatusNotImplemented)
		return
	}
	
	// Get query parameters
	query := r.URL.Query().Get("q")
	if query == "" {
		c.SetInvalidParam("q")
		return
	}
	
	// Parse page and per_page
	page := 0
	if r.URL.Query().Get("page") != "" {
		var err error
		page, err = model.ParseInt(r.URL.Query().Get("page"), 10)
		if err != nil {
			c.SetInvalidParam("page")
			return
		}
	}
	
	perPage := 20
	if r.URL.Query().Get("per_page") != "" {
		var err error
		perPage, err = model.ParseInt(r.URL.Query().Get("per_page"), 10)
		if err != nil {
			c.SetInvalidParam("per_page")
			return
		}
	}
	
	// Determine if this is an OR search
	isOrSearch := false
	if r.URL.Query().Get("is_or_search") == "true" {
		isOrSearch = true
	}
	
	// Create audit record
	auditRec := c.App.MakeAuditRecord("searchPostsES", audit.Fail)
	defer c.App.LogAuditRec(auditRec, c.AppContext.Session().UserId)
	audit.AddEventParameter(auditRec, "query", query)
	
	// Start the search
	startTime := time.Now()
	
	// Build search parameters
	params := model.ParseSearchParams(query, 0)
	for _, param := range params {
		param.OrTerms = isOrSearch
	}
	
	teamId := ""
	if len(c.AppContext.Session().TeamIds) > 0 {
		teamId = c.AppContext.Session().TeamIds[0]
	}
	
	// Perform the search
	results, err := c.App.SearchPostsForUser(c.AppContext, query, c.AppContext.Session().UserId, teamId, isOrSearch, false, 0, page, perPage)
	
	elapsedTime := float64(time.Since(startTime)) / float64(time.Second)
	metrics := c.App.Metrics()
	if metrics != nil {
		metrics.IncrementPostsSearchCounter()
		metrics.ObservePostsSearchDuration(elapsedTime)
	}
	
	if err != nil {
		c.Err = err
		return
	}
	
	clientPostList := c.App.PreparePostListForClient(c.AppContext, results.PostList)
	clientPostList, err = c.App.SanitizePostListMetadataForUser(c.AppContext, clientPostList, c.AppContext.Session().UserId)
	if err != nil {
		c.Err = err
		return
	}
	
	auditRec.Success()
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Write([]byte(clientPostList.ToJson()))
}

// testElasticsearchConfig is the handler for testing the Elasticsearch configuration
func testElasticsearchConfig(c *web.Context, w http.ResponseWriter, r *http.Request) {
	var config model.Config
	if jsonErr := json.NewDecoder(r.Body).Decode(&config); jsonErr != nil {
		c.Err = model.NewAppError("testElasticsearchConfig", "api.elasticsearch.test_config.parse_config.app_error", nil, jsonErr.Error(), http.StatusBadRequest)
		return
	}
	
	// Test connection
	esService := c.App.Srv().ElasticsearchService()
	if esService == nil {
		c.Err = model.NewAppError("testElasticsearchConfig", "api.elasticsearch.test_config.service_not_initialized", nil, "", http.StatusInternalServerError)
		return
	}
	
	err := esService.TestConnection(&config)
	if err != nil {
		c.Err = model.NewAppError("testElasticsearchConfig", "api.elasticsearch.test_config.connection_error", nil, err.Error(), http.StatusBadRequest)
		return
	}
	
	w.Write([]byte(model.MapToJson(map[string]string{"status": "OK"})))
}

// indexPostBatch is the handler for batch indexing posts
func indexPostBatch(c *web.Context, w http.ResponseWriter, r *http.Request) {
	c.RequireTeamId()
	if c.Err != nil {
		return
	}
	
	var opts struct {
		StartTime int64 `json:"start_time"`
		EndTime   int64 `json:"end_time"`
		Limit     int   `json:"limit"`
	}
	
	if jsonErr := json.NewDecoder(r.Body).Decode(&opts); jsonErr != nil {
		c.Err = model.NewAppError("indexPostBatch", "api.elasticsearch.index_post_batch.parse_options.app_error", nil, jsonErr.Error(), http.StatusBadRequest)
		return
	}
	
	// Get the service
	esService := c.App.Srv().ElasticsearchService()
	if esService == nil || !esService.IsActive() {
		c.Err = model.NewAppError("indexPostBatch", "api.elasticsearch.index_post_batch.service_disabled", nil, "", http.StatusInternalServerError)
		return
	}
	
	// Set default values
	if opts.Limit <= 0 {
		opts.Limit = 1000
	}
	
	if opts.StartTime <= 0 {
		// If no start time provided, use a very old time to index everything
		opts.StartTime = model.GetMillisForTime(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	}
	
	if opts.EndTime <= 0 {
		// If no end time provided, use current time
		opts.EndTime = model.GetMillis()
	}
	
	// Query posts from database
	posts, err := c.App.GetPostsForIndexing(c.AppContext, opts.StartTime, opts.EndTime, opts.Limit)
	if err != nil {
		c.Err = model.NewAppError("indexPostBatch", "api.elasticsearch.index_post_batch.get_posts.app_error", nil, err.Error(), http.StatusInternalServerError)
		return
	}
	
	if len(posts) == 0 {
		w.Write([]byte(model.MapToJson(map[string]interface{}{
			"status": "OK",
			"count":  0,
		})))
		return
	}
	
	// Index posts
	if err := esService.BatchIndexPosts(posts, c.Params.TeamId); err != nil {
		c.Err = model.NewAppError("indexPostBatch", "api.elasticsearch.index_post_batch.index_error", nil, err.Error(), http.StatusInternalServerError)
		return
	}
	
	// Force refresh to make documents available for search immediately
	if err := esService.RefreshIndex(); err != nil {
		c.App.GetLogger().Error("Error refreshing Elasticsearch index", mlog.Err(err))
	}
	
	w.Write([]byte(model.MapToJson(map[string]interface{}{
		"status": "OK",
		"count":  len(posts),
	})))
} 