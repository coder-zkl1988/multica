package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/integrations/testcapability"
)

// multica test artemis-mcp — the stdio proxy a test run mounts in front of
// Artemis's MCP server for one Android phone (TS-035). It is launched by the
// agent runtime from the task's MCP overlay, not typed by people, so it stays
// out of the help listing.
var testArtemisMCPCmd = &cobra.Command{
	Use:    "artemis-mcp --serial <adb-serial> [--daemon-port <port>] -- <artemis-python> <artemis-mcp-server.py>",
	Short:  "Run Artemis's MCP server pinned to one Android phone",
	Hidden: true,
	Args:   cobra.MinimumNArgs(1),
	RunE:   runTestArtemisMCP,
}

func init() {
	testArtemisMCPCmd.Flags().String("serial", "", "adb serial of the phone every device call is pinned to")
	testArtemisMCPCmd.Flags().String("daemon-port", "", "port of the Artemis daemon shared by every case on this host (ARTEMIS_DAEMON_PORT)")
	testRunGroupCmd.AddCommand(testArtemisMCPCmd)
}

func runTestArtemisMCP(cmd *cobra.Command, args []string) error {
	serial, _ := cmd.Flags().GetString("serial")
	port, _ := cmd.Flags().GetString("daemon-port")
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return testcapability.RunArtemisProxy(ctx, testcapability.ArtemisProxyOptions{Serial: serial, DaemonPort: port}, args, os.Stdin, os.Stdout, os.Stderr)
}
