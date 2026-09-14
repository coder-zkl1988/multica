package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var mcpCmd = newMCPCommand()

func newMCPCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Manage Multica MCP adapters",
	}
	setupCmd := &cobra.Command{
		Use:   "setup",
		Short: "Print MCP client configuration",
	}
	setupDesignCmd := &cobra.Command{
		Use:   "design",
		Short: "Configure the Design MCP adapter",
		Args:  cobra.NoArgs,
		RunE:  runMCPSetupDesign,
	}
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show MCP adapter status",
	}
	statusDesignCmd := &cobra.Command{
		Use:   "design",
		Short: "Show Design MCP adapter status",
		Args:  cobra.NoArgs,
		RunE:  runMCPStatusDesign,
	}
	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Run an MCP adapter over stdio",
	}
	serveDesignCmd := &cobra.Command{
		Use:   "design",
		Short: "Run the Design MCP adapter over stdio",
		Args:  cobra.NoArgs,
		RunE:  runMCPServeDesign,
	}
	setupCmd.AddCommand(setupDesignCmd)
	statusCmd.AddCommand(statusDesignCmd)
	serveCmd.AddCommand(serveDesignCmd)
	cmd.AddCommand(setupCmd)
	cmd.AddCommand(statusCmd)
	cmd.AddCommand(serveCmd)
	return cmd
}

func runMCPSetupDesign(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if client.Token == "" {
		return fmt.Errorf("not_authenticated: run 'multica login'")
	}
	if client.WorkspaceID == "" {
		return fmt.Errorf("workspace_missing: run 'multica workspace switch <workspace>' or pass --workspace-id")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := validateDesignMCPAuth(ctx, client); err != nil {
		return err
	}
	if err := validateDesignMCPWorkspace(ctx, client); err != nil {
		return err
	}
	snippet := designMCPClientSnippet(resolveProfile(cmd))
	encoded, err := json.MarshalIndent(snippet, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Design MCP is ready.")
	fmt.Fprintln(cmd.OutOrStdout(), "")
	fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
	return nil
}

func runMCPStatusDesign(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	status := "authenticated"
	if client.Token == "" {
		status = "not_authenticated"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "server_url: %s\n", client.BaseURL)
	fmt.Fprintf(cmd.OutOrStdout(), "workspace_id: %s\n", valueOrDefault(client.WorkspaceID, "(not set)"))
	fmt.Fprintf(cmd.OutOrStdout(), "auth: %s\n", status)
	return nil
}

func runMCPServeDesign(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if client.Token == "" {
		return fmt.Errorf("not_authenticated: run 'multica login'")
	}
	if client.WorkspaceID == "" {
		return fmt.Errorf("workspace_missing: run 'multica workspace switch <workspace>' or pass --workspace-id")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if strings.HasPrefix(client.Token, "mul_") {
		_ = client.PostJSON(ctx, "/api/tokens/current/renew", map[string]any{}, nil)
	}
	if err := validateDesignMCPAuth(ctx, client); err != nil {
		return err
	}
	if err := validateDesignMCPWorkspace(ctx, client); err != nil {
		return err
	}
	adapter := &designMCPAdapter{client: client, rootDir: "."}
	return newDesignMCPServer(adapter).serve(cmd.InOrStdin(), cmd.OutOrStdout())
}

func validateDesignMCPAuth(ctx context.Context, client interface {
	GetJSON(context.Context, string, any) error
}) error {
	var me struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := client.GetJSON(ctx, "/api/me", &me); err != nil {
		return fmt.Errorf("not_authenticated: run 'multica login': %w", err)
	}
	return nil
}

func validateDesignMCPWorkspace(ctx context.Context, client *cli.APIClient) error {
	var workspaces []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := client.GetJSON(ctx, "/api/workspaces", &workspaces); err != nil {
		return fmt.Errorf("workspace_missing: failed to list workspaces: %w", err)
	}
	for _, workspace := range workspaces {
		if workspace.ID == client.WorkspaceID {
			return nil
		}
	}
	return fmt.Errorf("workspace_missing: configured workspace is not accessible")
}

func designMCPClientSnippet(profile string) map[string]any {
	args := []string{"mcp", "serve", "design"}
	if profile != "" {
		args = []string{"--profile", profile, "mcp", "serve", "design"}
	}
	return map[string]any{
		"mcpServers": map[string]any{
			"multica-design": map[string]any{
				"command": "multica",
				"args":    args,
			},
		},
	}
}
