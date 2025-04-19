// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package elasticsearch

import (
	"fmt"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/shared/mlog"
	"github.com/mattermost/mattermost/server/v8/channels/app"
)

// IndexPostsCmd is the CLI command function for batch indexing posts
func IndexPostsCmd(a *app.App, args []string) {
	if !*a.Config().ElasticsearchSettings.EnableIndexing {
		fmt.Println("Elasticsearch indexing is disabled. Please enable it in the system console.")
		return
	}

	esService := a.Srv().ElasticsearchService()
	if esService == nil || !esService.IsActive() {
		fmt.Println("Elasticsearch service is not available or not active.")
		return
	}

	startTime := model.GetMillisForTime(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	endTime := model.GetMillis()
	batchSize := 1000
	totalIndexed := 0

	// Parse args
	if len(args) > 0 {
		// Parse batch size
		if size, err := model.ParseInt(args[0], 10); err == nil && size > 0 {
			batchSize = size
		}
	}

	// Get teams
	teams, err := a.GetAllTeams()
	if err != nil {
		fmt.Println("Error getting teams:", err.Error())
		return
	}

	if len(teams) == 0 {
		fmt.Println("No teams found.")
		return
	}

	fmt.Println("Starting to index posts...")
	fmt.Printf("Batch size: %d\n", batchSize)

	for _, team := range teams {
		fmt.Printf("Indexing posts for team %s (%s)...\n", team.Name, team.Id)

		for {
			// Get posts
			posts, appErr := a.GetPostsForIndexing(a.RequestId(), startTime, endTime, batchSize)
			if appErr != nil {
				fmt.Println("Error getting posts:", appErr.Error())
				return
			}

			if len(posts) == 0 {
				fmt.Println("No more posts to index for this team.")
				break
			}

			// Index posts
			if err := esService.BatchIndexPosts(posts, team.Id); err != nil {
				fmt.Println("Error indexing posts:", err.Error())
				return
			}

			totalIndexed += len(posts)
			fmt.Printf("Indexed %d posts (total: %d)\n", len(posts), totalIndexed)

			// Update the start time to the latest post indexed
			latestPost := posts[len(posts)-1]
			startTime = latestPost.CreateAt + 1

			// Force refresh index
			if err := esService.RefreshIndex(); err != nil {
				a.Log().Error("Error refreshing Elasticsearch index", mlog.Err(err))
			}

			// Pause to avoid overwhelming the server
			time.Sleep(1 * time.Second)
		}
	}

	fmt.Printf("Finished indexing posts. Total indexed: %d\n", totalIndexed)
} 