// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package elasticsearch

import (
	"fmt"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/shared/request"
	"github.com/mattermost/mattermost/server/v8/platform/services/searchengine"
)

// SearchAdapter implements the SearchEngineInterface for Elasticsearch
type SearchAdapter struct {
	service *Service
}

// NewSearchAdapter creates a new Elasticsearch search adapter
func NewSearchAdapter(service *Service) *SearchAdapter {
	return &SearchAdapter{
		service: service,
	}
}

// Start initializes the search engine
func (a *SearchAdapter) Start() *model.AppError {
	// Service was already started when created
	return nil
}

// Stop stops the search engine
func (a *SearchAdapter) Stop() *model.AppError {
	// Nothing to stop
	return nil
}

// GetFullVersion returns the full version string
func (a *SearchAdapter) GetFullVersion() string {
	return "elasticsearch-os-v1.0.0"
}

// GetVersion returns the version as an integer
func (a *SearchAdapter) GetVersion() int {
	return 1
}

// GetPlugins returns the list of plugins
func (a *SearchAdapter) GetPlugins() []string {
	return []string{"elasticsearch-posts"}
}

// UpdateConfig updates the search engine configuration
func (a *SearchAdapter) UpdateConfig(cfg *model.Config) {
	if err := a.service.UpdateConfig(cfg); err != nil {
		// Just log the error, don't fail
		a.service.logger.Error("Failed to update Elasticsearch config", err)
	}
}

// GetName returns the name of the search engine
func (a *SearchAdapter) GetName() string {
	return "elasticsearch-os"
}

// IsEnabled returns true if the search engine is enabled
func (a *SearchAdapter) IsEnabled() bool {
	return a.service.IsActive()
}

// IsActive returns true if the search engine is active
func (a *SearchAdapter) IsActive() bool {
	return a.service.IsActive()
}

// IsIndexingEnabled returns true if indexing is enabled
func (a *SearchAdapter) IsIndexingEnabled() bool {
	return a.service.IsActive()
}

// IsSearchEnabled returns true if searching is enabled
func (a *SearchAdapter) IsSearchEnabled() bool {
	return a.service.IsActive()
}

// IsAutocompletionEnabled returns true if autocomplete is enabled
func (a *SearchAdapter) IsAutocompletionEnabled() bool {
	// Autocomplete is not currently implemented
	return false
}

// IsIndexingSync returns true if indexing is synchronous
func (a *SearchAdapter) IsIndexingSync() bool {
	return false
}

// IndexPost indexes a post
func (a *SearchAdapter) IndexPost(post *model.Post, teamId string) *model.AppError {
	if !a.service.IsActive() {
		return model.NewAppError("SearchAdapter.IndexPost", "elasticsearch.index_post.error", nil, "Elasticsearch is not active", 0)
	}

	err := a.service.IndexPost(post, teamId)
	if err != nil {
		return model.NewAppError("SearchAdapter.IndexPost", "elasticsearch.index_post.error", nil, err.Error(), 0)
	}

	return nil
}

// SearchPosts searches for posts matching the given parameters
func (a *SearchAdapter) SearchPosts(channels model.ChannelList, searchParams []*model.SearchParams, page, perPage int) ([]string, model.PostSearchMatches, *model.AppError) {
	if !a.service.IsActive() {
		return nil, nil, model.NewAppError("SearchAdapter.SearchPosts", "elasticsearch.search_posts.error", nil, "Elasticsearch is not active", 0)
	}

	postIds, matches, err := a.service.SearchPosts(channels, searchParams, page, perPage)
	if err != nil {
		return nil, nil, model.NewAppError("SearchAdapter.SearchPosts", "elasticsearch.search_posts.error", nil, err.Error(), 0)
	}

	return postIds, matches, nil
}

// DeletePost deletes a post from the search index
func (a *SearchAdapter) DeletePost(post *model.Post) *model.AppError {
	if !a.service.IsActive() {
		return model.NewAppError("SearchAdapter.DeletePost", "elasticsearch.delete_post.error", nil, "Elasticsearch is not active", 0)
	}

	err := a.service.DeletePost(post.Id)
	if err != nil {
		return model.NewAppError("SearchAdapter.DeletePost", "elasticsearch.delete_post.error", nil, err.Error(), 0)
	}

	return nil
}

// DeleteChannelPosts deletes all posts from a channel from the search index
func (a *SearchAdapter) DeleteChannelPosts(rctx request.CTX, channelID string) *model.AppError {
	// Not implemented yet
	// This would require a separate API to delete documents by query
	return nil
}

// DeleteUserPosts deletes all posts from a user from the search index
func (a *SearchAdapter) DeleteUserPosts(rctx request.CTX, userID string) *model.AppError {
	// Not implemented yet
	return nil
}

// IndexChannel indexes a channel
func (a *SearchAdapter) IndexChannel(rctx request.CTX, channel *model.Channel, userIDs, teamMemberIDs []string) *model.AppError {
	// Channel indexing is not implemented
	return nil
}

// SearchChannels searches for channels
func (a *SearchAdapter) SearchChannels(teamId, userID, term string, isGuest, includeDeleted bool) ([]string, *model.AppError) {
	// Channel search is not implemented
	return []string{}, nil
}

// DeleteChannel deletes a channel from the search index
func (a *SearchAdapter) DeleteChannel(channel *model.Channel) *model.AppError {
	// Channel indexing is not implemented
	return nil
}

// IndexUser indexes a user
func (a *SearchAdapter) IndexUser(rctx request.CTX, user *model.User, teamsIds, channelsIds []string) *model.AppError {
	// User indexing is not implemented
	return nil
}

// SearchUsersInChannel searches for users in a channel
func (a *SearchAdapter) SearchUsersInChannel(teamId, channelId string, restrictedToChannels []string, term string, options *model.UserSearchOptions) ([]string, []string, *model.AppError) {
	// User search is not implemented
	return []string{}, []string{}, nil
}

// SearchUsersInTeam searches for users in a team
func (a *SearchAdapter) SearchUsersInTeam(teamId string, restrictedToChannels []string, term string, options *model.UserSearchOptions) ([]string, *model.AppError) {
	// User search is not implemented
	return []string{}, nil
}

// DeleteUser deletes a user from the search index
func (a *SearchAdapter) DeleteUser(user *model.User) *model.AppError {
	// User indexing is not implemented
	return nil
}

// IndexFile indexes a file
func (a *SearchAdapter) IndexFile(file *model.FileInfo, channelId string) *model.AppError {
	// File indexing is not implemented
	return nil
}

// SearchFiles searches for files
func (a *SearchAdapter) SearchFiles(channels model.ChannelList, searchParams []*model.SearchParams, page, perPage int) ([]string, *model.AppError) {
	// File search is not implemented
	return []string{}, nil
}

// DeleteFile deletes a file from the search index
func (a *SearchAdapter) DeleteFile(fileID string) *model.AppError {
	// File indexing is not implemented
	return nil
}

// DeletePostFiles deletes all files associated with a post from the search index
func (a *SearchAdapter) DeletePostFiles(rctx request.CTX, postID string) *model.AppError {
	// File indexing is not implemented
	return nil
}

// DeleteUserFiles deletes all files associated with a user from the search index
func (a *SearchAdapter) DeleteUserFiles(rctx request.CTX, userID string) *model.AppError {
	// File indexing is not implemented
	return nil
}

// DeleteFilesBatch deletes a batch of files from the search index
func (a *SearchAdapter) DeleteFilesBatch(rctx request.CTX, endTime, limit int64) *model.AppError {
	// File indexing is not implemented
	return nil
}

// TestConfig tests the search engine configuration
func (a *SearchAdapter) TestConfig(rctx request.CTX, cfg *model.Config) *model.AppError {
	err := a.service.TestConnection(cfg)
	if err != nil {
		return model.NewAppError("SearchAdapter.TestConfig", "elasticsearch.test_config.error", nil, err.Error(), 0)
	}

	return nil
}

// PurgeIndexes purges all search indexes
func (a *SearchAdapter) PurgeIndexes(rctx request.CTX) *model.AppError {
	// Not implemented
	return nil
}

// PurgeIndexList purges specific indexes
func (a *SearchAdapter) PurgeIndexList(rctx request.CTX, indexes []string) *model.AppError {
	// Not implemented
	return nil
}

// RefreshIndexes refreshes all search indexes
func (a *SearchAdapter) RefreshIndexes(rctx request.CTX) *model.AppError {
	if !a.service.IsActive() {
		return model.NewAppError("SearchAdapter.RefreshIndexes", "elasticsearch.refresh_indexes.error", nil, "Elasticsearch is not active", 0)
	}

	err := a.service.RefreshIndex()
	if err != nil {
		return model.NewAppError("SearchAdapter.RefreshIndexes", "elasticsearch.refresh_indexes.error", nil, err.Error(), 0)
	}

	return nil
}

// DataRetentionDeleteIndexes deletes data older than the retention policy
func (a *SearchAdapter) DataRetentionDeleteIndexes(rctx request.CTX, cutoff time.Time) *model.AppError {
	// Data retention is not implemented
	return nil
} 