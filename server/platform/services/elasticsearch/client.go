// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/typedapi/core/search"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types/enums/highlighterencoder"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types/enums/operator"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types/enums/sortorder"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/shared/mlog"
	"github.com/mattermost/mattermost/server/public/shared/request"
)

const (
	MessagesIndexName    = "mattermost_messages"
	DefaultBatchSize     = 100
	DefaultTimeoutSecs   = 30
	DefaultFuzzyDistance = 1
)

// ESMessage represents the structure of a message in Elasticsearch
type ESMessage struct {
	Id          string    `json:"id"`
	ChannelId   string    `json:"channel_id"`
	UserId      string    `json:"user_id"`
	Message     string    `json:"message"`
	CreateAt    int64     `json:"create_at"`
	UpdateAt    int64     `json:"update_at"`
	DeleteAt    int64     `json:"delete_at"`
	Type        string    `json:"type"`
	Hashtags    string    `json:"hashtags"`
	TeamId      string    `json:"team_id"`
	ParentId    string    `json:"parent_id"`
	RootId      string    `json:"root_id"`
}

// Client represents an Elasticsearch client
type Client struct {
	esClient   *elasticsearch.TypedClient
	config     *model.Config
	configLock sync.RWMutex
	ready      int32
	logger     mlog.LoggerIFace
}

// NewClient creates a new Elasticsearch client
func NewClient(config *model.Config, logger mlog.LoggerIFace) (*Client, error) {
	client := &Client{
		config: config,
		logger: logger,
	}

	if err := client.Connect(); err != nil {
		return nil, err
	}

	return client, nil
}

// Connect establishes a connection to Elasticsearch
func (c *Client) Connect() error {
	c.configLock.RLock()
	connectionUrl := *c.config.ElasticsearchSettings.ConnectionURL
	username := *c.config.ElasticsearchSettings.Username
	password := *c.config.ElasticsearchSettings.Password
	sniff := *c.config.ElasticsearchSettings.Sniff
	timeoutSeconds := *c.config.ElasticsearchSettings.RequestTimeoutSeconds
	c.configLock.RUnlock()

	cfg := elasticsearch.Config{
		Addresses: []string{connectionUrl},
		Username:  username,
		Password:  password,
	}

	if timeoutSeconds <= 0 {
		timeoutSeconds = DefaultTimeoutSecs
	}
	cfg.Transport = &http.Transport{
		ResponseHeaderTimeout: time.Duration(timeoutSeconds) * time.Second,
	}

	esClient, err := elasticsearch.NewTypedClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create Elasticsearch client: %w", err)
	}

	c.esClient = esClient
	atomic.StoreInt32(&c.ready, 1)
	
	// Check if we can connect to Elasticsearch
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()
	
	_, err = esClient.Info().Do(ctx)
	if err != nil {
		atomic.StoreInt32(&c.ready, 0)
		return fmt.Errorf("failed to connect to Elasticsearch: %w", err)
	}

	return nil
}

// IsReady returns true if the Elasticsearch client is ready to use
func (c *Client) IsReady() bool {
	return atomic.LoadInt32(&c.ready) == 1
}

// UpdateConfig updates the client configuration
func (c *Client) UpdateConfig(config *model.Config) error {
	c.configLock.Lock()
	defer c.configLock.Unlock()
	
	c.config = config
	
	// If the connection settings changed, reconnect
	return c.Connect()
}

// CreateMessageIndex creates the index for storing messages
func (c *Client) CreateMessageIndex() error {
	if !c.IsReady() {
		return fmt.Errorf("Elasticsearch client is not ready")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(DefaultTimeoutSecs)*time.Second)
	defer cancel()

	exists, err := c.esClient.Indices.Exists(MessagesIndexName).Do(ctx)
	if err != nil {
		return fmt.Errorf("failed to check if index exists: %w", err)
	}

	if exists {
		c.logger.Info("Elasticsearch index already exists", mlog.String("index", MessagesIndexName))
		return nil
	}

	// Define the index mapping
	mapping := `{
		"mappings": {
			"properties": {
				"id": { "type": "keyword" },
				"channel_id": { "type": "keyword" },
				"user_id": { "type": "keyword" },
				"message": { "type": "text" },
				"create_at": { "type": "date" },
				"update_at": { "type": "date" },
				"delete_at": { "type": "date" },
				"type": { "type": "keyword" },
				"hashtags": { "type": "text" },
				"team_id": { "type": "keyword" },
				"parent_id": { "type": "keyword" },
				"root_id": { "type": "keyword" }
			}
		}
	}`

	res, err := c.esClient.Indices.Create(MessagesIndexName).
		Raw(strings.NewReader(mapping)).
		Do(ctx)
	if err != nil {
		return fmt.Errorf("failed to create index: %w", err)
	}

	if res.Acknowledged {
		c.logger.Info("Successfully created Elasticsearch index", mlog.String("index", MessagesIndexName))
	}

	return nil
}

// FromPost converts a Post to an ESMessage
func (c *Client) FromPost(post *model.Post, teamId string) *ESMessage {
	return &ESMessage{
		Id:        post.Id,
		ChannelId: post.ChannelId,
		UserId:    post.UserId,
		Message:   post.Message,
		CreateAt:  post.CreateAt,
		UpdateAt:  post.UpdateAt,
		DeleteAt:  post.DeleteAt,
		Type:      post.Type,
		Hashtags:  post.Hashtags,
		TeamId:    teamId,
		ParentId:  post.ParentId,
		RootId:    post.RootId,
	}
}

// IndexPost indexes a post in Elasticsearch
func (c *Client) IndexPost(post *model.Post, teamId string) error {
	if !c.IsReady() {
		return fmt.Errorf("Elasticsearch client is not ready")
	}

	c.configLock.RLock()
	timeoutSeconds := *c.config.ElasticsearchSettings.RequestTimeoutSeconds
	c.configLock.RUnlock()

	if timeoutSeconds <= 0 {
		timeoutSeconds = DefaultTimeoutSecs
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	message := c.FromPost(post, teamId)

	_, err := c.esClient.Index(MessagesIndexName).
		Id(post.Id).
		Document(message).
		Do(ctx)

	if err != nil {
		return fmt.Errorf("failed to index post: %w", err)
	}

	return nil
}

// DeletePost deletes a post from the Elasticsearch index
func (c *Client) DeletePost(postId string) error {
	if !c.IsReady() {
		return fmt.Errorf("Elasticsearch client is not ready")
	}

	c.configLock.RLock()
	timeoutSeconds := *c.config.ElasticsearchSettings.RequestTimeoutSeconds
	c.configLock.RUnlock()

	if timeoutSeconds <= 0 {
		timeoutSeconds = DefaultTimeoutSecs
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	_, err := c.esClient.Delete(MessagesIndexName, postId).Do(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete post: %w", err)
	}

	return nil
}

// RefreshIndex forces a refresh of the Elasticsearch index
func (c *Client) RefreshIndex() error {
	if !c.IsReady() {
		return fmt.Errorf("Elasticsearch client is not ready")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(DefaultTimeoutSecs)*time.Second)
	defer cancel()

	_, err := c.esClient.Indices.Refresh().Index(MessagesIndexName).Do(ctx)
	if err != nil {
		return fmt.Errorf("failed to refresh index: %w", err)
	}

	return nil
}

// SearchPosts searches for posts based on search parameters
func (c *Client) SearchPosts(channels model.ChannelList, searchParams []*model.SearchParams, page, perPage int) ([]string, model.PostSearchMatches, error) {
	if !c.IsReady() {
		return nil, nil, fmt.Errorf("Elasticsearch client is not ready")
	}

	c.configLock.RLock()
	timeoutSeconds := *c.config.ElasticsearchSettings.RequestTimeoutSeconds
	c.configLock.RUnlock()

	if timeoutSeconds <= 0 {
		timeoutSeconds = DefaultTimeoutSecs
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	// Build channel filter
	var channelFilters []types.Query
	for _, channel := range channels {
		channelQuery := types.TermQuery{
			Field: model.NewPointer("channel_id"),
			Value: channel.Id,
		}
		channelFilters = append(channelFilters, channelQuery)
	}
	channelFilterQuery := types.BoolQuery{
		Should: channelFilters,
		MinimumShouldMatch: model.NewPointer(1),
	}

	// Build search query from parameters
	var termQueries []types.Query
	var notTermQueries []types.Query
	var orOperator bool

	for _, params := range searchParams {
		orOperator = params.OrTerms

		if params.Terms != "" {
			termQuery := types.MatchQuery{
				Field:     model.NewPointer("message"),
				Query:     params.Terms,
				Fuzziness: model.NewPointer(DefaultFuzzyDistance),
				Operator:  model.NewPointer(operator.And),
			}
			
			if orOperator {
				termQuery.Operator = model.NewPointer(operator.Or)
			}
			
			termQueries = append(termQueries, termQuery)
		}

		if params.ExcludedTerms != "" {
			notTermQuery := types.MatchQuery{
				Field: model.NewPointer("message"),
				Query: params.ExcludedTerms,
			}
			notTermQueries = append(notTermQueries, notTermQuery)
		}

		// Additional filters can be added here (from_users, in_channels, date filters, etc.)
	}

	mainQuery := types.BoolQuery{
		Must:    []types.Query{channelFilterQuery},
		MustNot: notTermQueries,
	}

	if len(termQueries) > 0 {
		if orOperator {
			mainQuery.Should = termQueries
			mainQuery.MinimumShouldMatch = model.NewPointer(1)
		} else {
			mainQuery.Must = append(mainQuery.Must, termQueries...)
		}
	}

	// Add highlighting
	highlight := types.Highlight{
		Encoder: model.NewPointer(highlighterencoder.Html),
		Fields: map[string]types.HighlightField{
			"message": {
				NumberOfFragments: model.NewPointer(0),
				FragmentSize:      model.NewPointer(0),
			},
		},
		PreTags:  []string{"<span class='search-highlight'>"},
		PostTags: []string{"</span>"},
	}

	// Create search request
	searchRequest := search.Request{
		Query:     mainQuery,
		Highlight: &highlight,
		Sort: []types.SortCombinations{
			types.SortOptions{
				SortOptions: map[string]types.FieldSort{
					"create_at": {
						Order: model.NewPointer(sortorder.Desc),
					},
				},
			},
		},
		From: model.NewPointer(page * perPage),
		Size: model.NewPointer(perPage),
	}

	// Execute search
	res, err := c.esClient.Search().
		Index(MessagesIndexName).
		Request(&searchRequest).
		Do(ctx)

	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute search: %w", err)
	}

	postIds := make([]string, len(res.Hits.Hits))
	matches := make(model.PostSearchMatches)

	// Process search results
	for i, hit := range res.Hits.Hits {
		var esMessage ESMessage
		if err := json.Unmarshal(hit.Source_, &esMessage); err != nil {
			c.logger.Error("Failed to unmarshal search result", mlog.Err(err))
			continue
		}

		postIds[i] = esMessage.Id

		// Extract highlighted terms
		if hit.Highlight != nil {
			if messageHighlights, ok := hit.Highlight["message"]; ok && len(messageHighlights) > 0 {
				// Process highlights
				matches[esMessage.Id] = []string{messageHighlights[0]}
			}
		}
	}

	return postIds, matches, nil
}

// BatchIndexPosts indexes multiple posts in a batch
func (c *Client) BatchIndexPosts(posts []*model.Post, teamId string) error {
	if !c.IsReady() {
		return fmt.Errorf("Elasticsearch client is not ready")
	}

	if len(posts) == 0 {
		return nil
	}

	c.configLock.RLock()
	timeoutSeconds := *c.config.ElasticsearchSettings.RequestTimeoutSeconds
	c.configLock.RUnlock()

	if timeoutSeconds <= 0 {
		timeoutSeconds = DefaultTimeoutSecs
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	var buf bytes.Buffer

	for _, post := range posts {
		// Add metadata line
		meta := map[string]any{
			"index": map[string]any{
				"_index": MessagesIndexName,
				"_id":    post.Id,
			},
		}
		
		if err := json.NewEncoder(&buf).Encode(meta); err != nil {
			return fmt.Errorf("failed to encode meta: %w", err)
		}

		// Add document data
		message := c.FromPost(post, teamId)
		if err := json.NewEncoder(&buf).Encode(message); err != nil {
			return fmt.Errorf("failed to encode document: %w", err)
		}
	}

	res, err := c.esClient.Bulk().Raw(&buf).Do(ctx)
	if err != nil {
		return fmt.Errorf("failed to perform bulk indexing: %w", err)
	}

	if res.Errors {
		// Process errors
		var errMsgs []string
		for _, item := range res.Items {
			for _, subItem := range item {
				if subItem.Status > 201 {
					errMsgs = append(errMsgs, fmt.Sprintf("error indexing document id=%s, status=%d, error=%s", 
						subItem.Id_, subItem.Status, subItem.Error))
				}
			}
		}
		if len(errMsgs) > 0 {
			return fmt.Errorf("errors during bulk indexing: %s", strings.Join(errMsgs, "; "))
		}
	}

	return nil
}

// TestConnection tests the connection to Elasticsearch
func (c *Client) TestConnection(config *model.Config) error {
	tempClient := &Client{
		config: config,
		logger: c.logger,
	}

	return tempClient.Connect()
} 