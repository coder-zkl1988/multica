package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var chatDocumentCmd = &cobra.Command{
	Use:   "document --url <feishu-docx-url>",
	Short: "Read a Feishu docx link with the current task's installation",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		raw, _ := cmd.Flags().GetString("url")
		if strings.TrimSpace(raw) == "" {
			return fmt.Errorf("--url is required")
		}
		client, err := newAPIClient(cmd)
		if err != nil {
			return err
		}
		ctx, cancel := cli.APIContext(context.Background())
		defer cancel()
		var response any
		path := "/api/chat/document?url=" + url.QueryEscape(raw)
		if err := client.GetJSON(ctx, path, &response); err != nil {
			return fmt.Errorf("read Feishu document: %w", err)
		}
		return cli.PrintJSON(os.Stdout, response)
	},
}

func init() {
	chatDocumentCmd.Flags().String("url", "", "HTTPS Feishu /docx/ link from the current conversation")
	chatCmd.AddCommand(chatDocumentCmd)
}
