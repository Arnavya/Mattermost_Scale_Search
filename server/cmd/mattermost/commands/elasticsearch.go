// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package commands

import (
	"fmt"
	"github.com/spf13/cobra"

	"github.com/mattermost/mattermost/server/v8/elasticsearch"
)

var ElasticsearchCmd = &cobra.Command{
	Use:   "elasticsearch",
	Short: "Elasticsearch related utilities",
}

var IndexCmd = &cobra.Command{
	Use:   "index",
	Short: "Index posts in Elasticsearch",
	Long:  "Index posts in the database to Elasticsearch. This command ensures all posts are correctly indexed for searching.",
	Example: `  # Index all posts
  mattermost elasticsearch index

  # Index posts with a batch size of 2000
  mattermost elasticsearch index --batch-size 2000`,
	RunE: elasticsearchIndexCmdF,
}

func init() {
	IndexCmd.Flags().IntP("batch-size", "b", 1000, "Batch size for indexing.")
	
	ElasticsearchCmd.AddCommand(
		IndexCmd,
	)
	
	RootCmd.AddCommand(
		ElasticsearchCmd,
	)
}

func elasticsearchIndexCmdF(command *cobra.Command, args []string) error {
	a, err := initDBCommandContext(getConfigDSN(command, ""), false)
	if err != nil {
		return err
	}
	defer a.Srv().Shutdown()

	batchSize, _ := command.Flags().GetInt("batch-size")
	if batchSize < 1 {
		batchSize = 1000
	}

	// Convert to string slice for compatibility with the ElasticsearchIndexCmd function
	strArgs := []string{}
	if batchSize != 1000 {
		strArgs = append(strArgs, fmt.Sprintf("%d", batchSize))
	}

	elasticsearch.IndexPostsCmd(a, strArgs)
	return nil
} 