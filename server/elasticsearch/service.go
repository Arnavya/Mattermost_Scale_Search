// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package elasticsearch

import (
	"fmt"
	"sync"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/shared/mlog"
	"github.com/mattermost/mattermost/server/public/shared/request"
)

// Service is the struct for the Elasticsearch service
type Service struct {
	client      *Client
	logger      mlog.LoggerIFace
	isActive    bool
	mutex       sync.RWMutex
	configStore *model.Config
}

// New creates a new Elasticsearch service
func New(configStore *model.Config, logger mlog.LoggerIFace) (*Service, error) {
	logger = logger.With(mlog.String("service", "elasticsearch"))

	service := &Service{
		logger:      logger,
		configStore: configStore,
	}

	// Don't try to connect if Elasticsearch is not enabled
	if !*configStore.ElasticsearchSettings.EnableIndexing {
		return service, nil
	}

	client, err := NewClient(configStore, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create elasticsearch client: %w", err)
	}

	service.client = client
	service.isActive = client.IsReady()

	// Create index if it doesn't exist
	if service.isActive {
		if err := client.CreateMessageIndex(); err != nil {
			return nil, fmt.Errorf("failed to create elasticsearch index: %w", err)
		}
	}

	return service, nil
}

// IsActive returns whether the service is active
func (s *Service) IsActive() bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.isActive && s.client != nil && s.client.IsReady()
}

// UpdateConfig updates the service's configuration
func (s *Service) UpdateConfig(config *model.Config) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if !*config.ElasticsearchSettings.EnableIndexing {
		s.isActive = false
		return nil
	}

	// Create client if it doesn't exist
	if s.client == nil {
		client, err := NewClient(config, s.logger)
		if err != nil {
			return fmt.Errorf("failed to create elasticsearch client: %w", err)
		}
		s.client = client
		s.isActive = client.IsReady()
	} else {
		// Update the existing client
		if err := s.client.UpdateConfig(config); err != nil {
			return fmt.Errorf("failed to update elasticsearch client: %w", err)
		}
		s.isActive = s.client.IsReady()
	}

	// Create index if it doesn't exist
	if s.isActive {
		if err := s.client.CreateMessageIndex(); err != nil {
			return fmt.Errorf("failed to create elasticsearch index: %w", err)
		}
	}

	s.configStore = config
	return nil
}

// IndexPost indexes a post
func (s *Service) IndexPost(post *model.Post, teamId string) error {
	if !s.IsActive() {
		return fmt.Errorf("elasticsearch service is not active")
	}

	return s.client.IndexPost(post, teamId)
}

// DeletePost deletes a post from the index
func (s *Service) DeletePost(postId string) error {
	if !s.IsActive() {
		return fmt.Errorf("elasticsearch service is not active")
	}

	return s.client.DeletePost(postId)
}

// BatchIndexPosts indexes multiple posts in a batch
func (s *Service) BatchIndexPosts(posts []*model.Post, teamId string) error {
	if !s.IsActive() {
		return fmt.Errorf("elasticsearch service is not active")
	}

	return s.client.BatchIndexPosts(posts, teamId)
}

// SearchPosts searches for posts
func (s *Service) SearchPosts(channels model.ChannelList, searchParams []*model.SearchParams, page, perPage int) ([]string, model.PostSearchMatches, error) {
	if !s.IsActive() {
		return nil, nil, fmt.Errorf("elasticsearch service is not active")
	}

	return s.client.SearchPosts(channels, searchParams, page, perPage)
}

// TestConnection tests the connection to Elasticsearch
func (s *Service) TestConnection(config *model.Config) error {
	// Create a temporary client to test the connection
	client := &Client{
		config: config,
		logger: s.logger,
	}

	return client.Connect()
}

// RefreshIndex forces a refresh of the Elasticsearch index
func (s *Service) RefreshIndex() error {
	if !s.IsActive() {
		return fmt.Errorf("elasticsearch service is not active")
	}

	return s.client.RefreshIndex()
} 