package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/cli"
)

const serviceTokenKeychainService = "ai.multica.cli.service-token"
const callbackHostFlag = "callback-host"

var loginTokenPrefixes = []string{"mul_", auth.CloudPATPrefix}

func validateLoginTokenPrefix(token string) error {
	for _, prefix := range loginTokenPrefixes {
		if strings.HasPrefix(token, prefix) {
			return nil
		}
	}
	return fmt.Errorf("invalid token format: must start with %s", strings.Join(loginTokenPrefixes, " or "))
}

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authenticate multica with Multica",
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current authentication status",
	RunE:  runAuthStatus,
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove stored authentication token",
	RunE:  runAuthLogout,
}

// callbackHostFlag lets users override the host/IP that goes into the OAuth
// cli_callback URL. Useful when the CLI sits behind a reverse proxy or the
// auto-detected LAN IP is not the one the browser can reach.
const callbackHostFlagHelp = "Host/IP the OAuth callback URL points at when the browser can reach this CLI directly. For SSH-only machines, use the printed tunnel hint instead."

func init() {
	authCmd.AddCommand(authStatusCmd)
	authCmd.AddCommand(authLogoutCmd)
}

func resolveToken(cmd *cobra.Command) string {
	if v := strings.TrimSpace(os.Getenv("MULTICA_TOKEN")); v != "" {
		return v
	}
	// Inside a daemon-managed task, never fall back to the user-global config
	// token: that silent fallback is how agent writes land as the wrong actor.
	// inDaemonManagedExecutionContext already covers the MULTICA_DAEMON_PORT
	// signal for subprocesses that lost MULTICA_AGENT_ID / MULTICA_TASK_ID.
	if inDaemonManagedExecutionContext() {
		return ""
	}
	profile := resolveProfile(cmd)
	cfg, _ := cli.LoadCLIConfigForProfile(profile)
	if cfg.Token == "" && cfg.ServiceTokenKeychainAccount != "" && runtime.GOOS == "darwin" {
		token, _ := readServiceToken(cfg.ServiceTokenKeychainAccount)
		return token
	}
	return cfg.Token
}

func resolveAppURL(cmd *cobra.Command) string {
	for _, key := range []string{"MULTICA_APP_URL", "FRONTEND_ORIGIN"} {
		if val := strings.TrimSpace(os.Getenv(key)); val != "" {
			return strings.TrimRight(val, "/")
		}
	}
	profile := resolveProfile(cmd)
	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err == nil && cfg.AppURL != "" {
		return strings.TrimRight(cfg.AppURL, "/")
	}
	fmt.Fprintln(os.Stderr, "No app URL configured. Run 'multica setup' first.")
	os.Exit(1)
	return "" // unreachable
}

func openBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "linux":
		cmd = "xdg-open"
		args = []string{url}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	return exec.Command(cmd, args...).Start()
}

func runAuthLogin(cmd *cobra.Command, args []string) error {
	serverURL := resolveServerURL(cmd)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	useSySSO, err := fetchUseSySSO(ctx, serverURL)
	if err != nil {
		return fmt.Errorf("fetch server auth mode: %w", err)
	}

	tokenChanged := cmd.Flags().Changed("token")
	serviceTokenChanged := cmd.Flags().Changed("service-token")
	if useSySSO {
		if tokenChanged {
			return errors.New("--token is unavailable when USE_SY_SSO=true; use browser SSO or --service-token")
		}
		serviceToken, _ := cmd.Flags().GetString("service-token")
		if serviceTokenChanged {
			if serviceToken == tokenPromptSentinel && len(args) == 1 {
				serviceToken = args[0]
			} else if len(args) != 0 {
				return fmt.Errorf("unexpected argument %q", args[0])
			}
			return runServiceTokenLogin(cmd, serviceToken)
		}
		if len(args) != 0 {
			return fmt.Errorf("unexpected argument %q", args[0])
		}
		return runAuthLoginBrowser(cmd)
	}

	if serviceTokenChanged {
		return errors.New("--service-token requires USE_SY_SSO=true; use browser login or --token")
	}
	if tokenChanged {
		tokenFlag, _ := cmd.Flags().GetString("token")
		if tokenFlag == tokenPromptSentinel && len(args) == 1 {
			tokenFlag = args[0]
		} else if len(args) != 0 {
			return fmt.Errorf("unexpected argument %q", args[0])
		}
		return runAuthLoginToken(cmd, tokenFlag)
	}
	if len(args) != 0 {
		return fmt.Errorf("unexpected argument %q", args[0])
	}
	return runLegacyAuthLoginBrowser(cmd)
}

func fetchUseSySSO(ctx context.Context, serverURL string) (bool, error) {
	var config struct {
		UseSySSO bool `json:"use_sy_sso"`
	}
	if err := cli.NewAPIClient(serverURL, "", "").GetJSON(ctx, "/api/config", &config); err != nil {
		return false, err
	}
	return config.UseSySSO, nil
}

func resolveCallbackBinding(flagHost, serverURL, appURL string, detectOutbound func(string) net.IP) (callbackHost, bindAddr string) {
	if host := strings.TrimSpace(flagHost); host != "" {
		return host, "0.0.0.0"
	}
	appIP := urlPrivateIP(appURL)
	if appIP == nil {
		return "localhost", "127.0.0.1"
	}
	cliIP := detectOutbound(serverURL)
	if cliIP == nil {
		return appIP.String(), "0.0.0.0"
	}
	if cliIP.Equal(appIP) {
		return "localhost", "127.0.0.1"
	}
	return cliIP.String(), "0.0.0.0"
}

func urlPrivateIP(rawURL string) net.IP {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	ip := net.ParseIP(parsed.Hostname())
	if ip == nil || !ip.IsPrivate() {
		return nil
	}
	return ip
}

func detectOutboundIP(serverURL string) net.IP {
	parsed, err := url.Parse(serverURL)
	if err != nil || parsed.Hostname() == "" {
		return nil
	}
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	conn, err := net.Dial("udp4", net.JoinHostPort(parsed.Hostname(), port))
	if err != nil {
		return nil
	}
	defer conn.Close()
	local, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || local.IP == nil {
		return nil
	}
	if v4 := local.IP.To4(); v4 != nil {
		return v4
	}
	return local.IP
}

func runLegacyAuthLoginBrowser(cmd *cobra.Command) error {
	serverURL := resolveServerURL(cmd)
	appURL := resolveAppURL(cmd)

	flagHost := callbackHostFlagValue(cmd)
	callbackHost, bindAddr := resolveCallbackBinding(flagHost, serverURL, appURL, detectOutboundIP)
	listener, err := net.Listen("tcp4", bindAddr+":0")
	if err != nil {
		return fmt.Errorf("could not start the local login callback server (used to receive the browser sign-in); a firewall or another process may be blocking local ports: %w", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	callbackURL := fmt.Sprintf("http://%s:%d/callback", callbackHost, port)
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		return fmt.Errorf("failed to generate state: %w", err)
	}
	state := hex.EncodeToString(stateBytes)
	loginURL := fmt.Sprintf("%s/login?cli_callback=%s&cli_state=%s", appURL, url.QueryEscape(callbackURL), url.QueryEscape(state))

	jwtCh := make(chan string, 1)
	errCh := make(chan error, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, "missing token", http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("state") != state {
			http.Error(w, "invalid state parameter", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(callbackSuccessHTML))
		jwtCh <- token
	})

	srv := &http.Server{Handler: mux}
	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	defer srv.Close()

	fmt.Fprintln(os.Stderr, "Opening browser to authenticate...")
	if err := openBrowser(loginURL); err != nil {
		fmt.Fprintln(os.Stderr, "Could not open browser automatically.")
	}
	fmt.Fprintf(os.Stderr, "If the browser didn't open, visit:\n  %s\n\nWaiting for authentication...\n", loginURL)

	var jwtToken string
	select {
	case jwtToken = <-jwtCh:
	case err := <-errCh:
		return fmt.Errorf("local server error: %w", err)
	case <-time.After(5 * time.Minute):
		return errors.New("timed out waiting for authentication")
	}

	client := cli.NewAPIClient(serverURL, "", jwtToken)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}
	var patResp struct {
		Token string `json:"token"`
	}
	if err := client.PostJSON(ctx, "/api/tokens", map[string]any{
		"name":            fmt.Sprintf("CLI (%s)", hostname),
		"expires_in_days": 90,
	}, &patResp); err != nil {
		return fmt.Errorf("failed to create access token: %w", err)
	}

	patClient := cli.NewAPIClient(serverURL, "", patResp.Token)
	var me struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := patClient.GetJSON(ctx, "/api/me", &me); err != nil {
		return fmt.Errorf("token verification failed: %w", err)
	}

	profile := resolveProfile(cmd)
	cfg, _ := cli.LoadCLIConfigForProfile(profile)
	cfg.WorkspaceID = ""
	cfg.Token = patResp.Token
	cfg.ServiceTokenKeychainAccount = ""
	cfg.ServerURL = serverURL
	cfg.AppURL = appURL
	if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Authenticated as %s (%s)\nToken saved to config.\n", me.Name, me.Email)
	return nil
}

func runAuthLoginBrowser(cmd *cobra.Command) error {
	serverURL := resolveServerURL(cmd)
	appURL := resolveAppURL(cmd)
	flagHost := callbackHostFlagValue(cmd)
	callbackHost, bindAddr := resolveCallbackBinding(flagHost, serverURL, appURL, detectOutboundIP)
	listener, err := net.Listen("tcp4", bindAddr+":0")
	if err != nil {
		return fmt.Errorf("failed to start local server: %w", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	callbackURL := fmt.Sprintf("http://%s:%d/callback", callbackHost, port)
	state, err := randomBase64URL(24)
	if err != nil {
		return err
	}
	verifier, err := randomBase64URL(32)
	if err != nil {
		return err
	}
	challengeBytes := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeBytes[:])
	loginURL := buildSSOAuthorizeURL(serverURL, callbackURL, state, challenge)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "invalid state parameter", http.StatusBadRequest)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing authorization code", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(callbackSuccessHTML))
		select {
		case codeCh <- code:
		default:
		}
	})

	srv := &http.Server{Handler: mux}
	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	defer srv.Close()

	// Open the browser.
	fmt.Fprintln(os.Stderr, "Opening browser to authenticate...")
	if err := openBrowser(loginURL); err != nil {
		fmt.Fprintf(os.Stderr, "Could not open browser automatically.\n")
	}
	fmt.Fprint(os.Stderr, browserLoginInstructions(loginURL, callbackHost, port, runningInSSHSession()))

	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		return fmt.Errorf("local server error: %w", err)
	case <-time.After(5 * time.Minute):
		return fmt.Errorf("timed out waiting for authentication")
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	exchange, err := exchangeSSOCode(ctx, serverURL, code, verifier, callbackURL)
	if err != nil {
		return cli.WithUserMessage("Sign-in did not complete: the server could not issue an access token for the CLI. Run `multica login` again.", err)
	}

	// Save to config. Reset workspace data on every login because the user or
	// server may have changed, so stale workspaces must not persist.
	profile := resolveProfile(cmd)
	cfg, _ := cli.LoadCLIConfigForProfile(profile)
	cfg.WorkspaceID = ""
	cfg.Token = exchange.Token
	cfg.ServiceTokenKeychainAccount = ""
	cfg.ServerURL = serverURL
	cfg.AppURL = appURL
	if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Authenticated as %s (%s)\nSession expires at %s.\n", exchange.User.Name, exchange.User.Email, exchange.ExpiresAt)
	return nil
}

func runningInSSHSession() bool {
	for _, key := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}
	return false
}

func callbackHostFlagValue(cmd *cobra.Command) string {
	for c := cmd; c != nil; c = c.Parent() {
		if value := nonEmptyFlagValue(c.Flags(), callbackHostFlag); value != "" {
			return value
		}
		if value := nonEmptyFlagValue(c.PersistentFlags(), callbackHostFlag); value != "" {
			return value
		}
		if value := nonEmptyFlagValue(c.InheritedFlags(), callbackHostFlag); value != "" {
			return value
		}
	}
	return ""
}

func nonEmptyFlagValue(flags *pflag.FlagSet, name string) string {
	if flag := flags.Lookup(name); flag != nil {
		return strings.TrimSpace(flag.Value.String())
	}
	return ""
}

func callbackHostIsLoopback(host string) bool {
	h := strings.Trim(strings.TrimSpace(host), "[]")
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func browserLoginInstructions(loginURL, callbackHost string, port int, remoteSSH bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "If the browser didn't open, visit:\n  %s\n", loginURL)
	if remoteSSH && callbackHostIsLoopback(callbackHost) {
		fmt.Fprintf(&b, "\nRemote SSH session detected. Before opening that URL on your local computer, forward the callback port in another terminal:\n  ssh -L %d:127.0.0.1:%d <user>@<remote-host>\nThen open the URL above in your local browser.\n", port, port)
	}
	fmt.Fprintln(&b, "\nWaiting for authentication...")
	return b.String()
}

func runAuthLoginToken(cmd *cobra.Command, providedToken string) error {
	// The prompt sentinel is what pflag substitutes for `--token` with no
	// value (see loginCmd init); treat it the same as an empty string so we
	// fall through to the interactive prompt.
	if providedToken == tokenPromptSentinel {
		providedToken = ""
	}
	token := strings.TrimSpace(providedToken)
	if token == "" {
		fmt.Print("Enter your personal access token: ")
		scanner := bufio.NewScanner(os.Stdin)
		if !scanner.Scan() {
			return fmt.Errorf("no input")
		}
		token = strings.TrimSpace(scanner.Text())
	}
	if token == "" {
		return fmt.Errorf("token is required")
	}
	if err := validateLoginTokenPrefix(token); err != nil {
		return err
	}

	serverURL := resolveLoginTokenServerURL(cmd)
	client := cli.NewAPIClient(serverURL, "", token)

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var me struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := client.GetJSON(ctx, "/api/me", &me); err != nil {
		return cli.WithUserMessage("Could not sign in with that token — make sure it is valid and not expired, then run `multica login --token <token>` again.", err)
	}

	profile := resolveProfile(cmd)
	cfg, _ := cli.LoadCLIConfigForProfile(profile)
	cfg.WorkspaceID = ""
	cfg.Token = token
	cfg.ServerURL = serverURL
	if cfg.AppURL == "" && serverURL == defaultCloudServerURL {
		cfg.AppURL = defaultCloudAppURL
	}
	if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Authenticated as %s (%s)\nToken saved to config.\n", me.Name, me.Email)
	return nil
}

type ssoCodeExchange struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
	User      struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"user"`
}

func randomBase64URL(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate browser authorization secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func buildSSOAuthorizeURL(serverURL, callbackURL, state, challenge string) string {
	query := url.Values{
		"client_id":             {"cli"},
		"redirect_uri":          {callbackURL},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	return strings.TrimRight(serverURL, "/") + "/auth/sso/authorize?" + query.Encode()
}

func exchangeSSOCode(ctx context.Context, serverURL, code, verifier, callbackURL string) (ssoCodeExchange, error) {
	var response ssoCodeExchange
	client := cli.NewAPIClient(serverURL, "", "")
	err := client.PostJSON(ctx, "/auth/sso/token", map[string]string{
		"grant_type":    "authorization_code",
		"code":          code,
		"code_verifier": verifier,
		"client_id":     "cli",
		"redirect_uri":  callbackURL,
	}, &response)
	if err != nil {
		return response, fmt.Errorf("exchange SSO authorization code: %w", err)
	}
	if response.Token == "" {
		return response, errors.New("SSO token response was empty")
	}
	return response, nil
}

func runServiceTokenLogin(cmd *cobra.Command, providedToken string) error {
	if providedToken == tokenPromptSentinel {
		providedToken = ""
	}
	token := strings.TrimSpace(providedToken)
	if token == "" {
		fmt.Print("Enter your ai_work service token: ")
		scanner := bufio.NewScanner(os.Stdin)
		if !scanner.Scan() {
			return errors.New("no input")
		}
		token = strings.TrimSpace(scanner.Text())
	}
	if !strings.HasPrefix(token, "msa_") {
		return errors.New("service token must start with msa_")
	}
	serverURL := resolveServerURL(cmd)
	client := cli.NewAPIClient(serverURL, "", token)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var me struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := client.GetJSON(ctx, "/api/me", &me); err != nil {
		return fmt.Errorf("invalid service token: %w", err)
	}

	profile := resolveProfile(cmd)
	storage, err := persistServiceToken(profile, serverURL, resolveAppURL(cmd), token, runtime.GOOS == "darwin", storeServiceToken)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Authenticated as %s (%s)\nService token saved to %s.\n", me.Name, me.Email, storage)
	return nil
}

func persistServiceToken(profile, serverURL, appURL, token string, useKeychain bool, store func(account, token string) error) (string, error) {
	account := profile
	if account == "" {
		account = "default"
	}
	cfg, _ := cli.LoadCLIConfigForProfile(profile)
	cfg.WorkspaceID = ""
	if useKeychain {
		if err := store(account, token); err != nil {
			return "", err
		}
		cfg.Token = ""
		cfg.ServiceTokenKeychainAccount = account
	} else {
		cfg.Token = token
		cfg.ServiceTokenKeychainAccount = ""
	}
	cfg.ServerURL = serverURL
	cfg.AppURL = appURL
	if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
		return "", fmt.Errorf("failed to save config: %w", err)
	}
	if useKeychain {
		return "macOS Keychain", nil
	}
	return "the user-only profile config", nil
}

func storeServiceToken(account, token string) error {
	command := exec.Command("/usr/bin/security", "add-generic-password", "-U", "-a", account, "-s", serviceTokenKeychainService, "-w")
	command.Stdin = strings.NewReader(token + "\n")
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("store service token in macOS Keychain: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func readServiceToken(account string) (string, error) {
	output, err := exec.Command("/usr/bin/security", "find-generic-password", "-a", account, "-s", serviceTokenKeychainService, "-w").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func runAuthStatus(cmd *cobra.Command, _ []string) error {
	token := resolveToken(cmd)
	serverURL := resolveServerURL(cmd)

	if token == "" {
		fmt.Fprintln(os.Stderr, "Not authenticated. Run 'multica login' to authenticate.")
		return nil
	}

	client := cli.NewAPIClient(serverURL, "", token)

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var me struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := client.GetJSON(ctx, "/api/me", &me); err != nil {
		fmt.Fprintf(os.Stderr, "Token is invalid or expired: %v\nRun 'multica login' to re-authenticate.\n", err)
		return nil
	}

	prefix := token
	if len(prefix) > 12 {
		prefix = prefix[:12] + "..."
	}

	fmt.Fprintf(os.Stderr, "Server:  %s\nUser:    %s (%s)\nToken:   %s\n", serverURL, me.Name, me.Email, prefix)
	return nil
}

const callbackSuccessHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Multica — Authenticated</title>
<style>
  *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
  @media (prefers-color-scheme: dark) {
    :root { --bg: #0b0b0f; --card-bg: #16161d; --border: rgba(255,255,255,0.10); --fg: #f5f5f5; --fg2: #a1a1aa; --accent: #22c55e; --accent-bg: rgba(34,197,94,0.12); }
  }
  @media (prefers-color-scheme: light) {
    :root { --bg: #f8f8fa; --card-bg: #ffffff; --border: rgba(0,0,0,0.08); --fg: #0f0f12; --fg2: #71717a; --accent: #16a34a; --accent-bg: rgba(22,163,74,0.08); }
  }
  body { font-family: -apple-system, "Segoe UI", Helvetica, Arial, sans-serif; background: var(--bg); color: var(--fg); display: flex; align-items: center; justify-content: center; min-height: 100vh; }
  .card { width: 100%; max-width: 380px; border: 1px solid var(--border); border-radius: 12px; background: var(--card-bg); padding: 40px 32px; text-align: center; }
  .icon-wrap { width: 48px; height: 48px; margin: 0 auto 24px; background: var(--accent-bg); border-radius: 50%; display: flex; align-items: center; justify-content: center; }
  .icon-wrap svg { width: 24px; height: 24px; color: var(--accent); }
  .brand { display: flex; align-items: center; justify-content: center; gap: 6px; margin-bottom: 8px; }
  .asterisk { display: inline-block; width: 14px; height: 14px; background: var(--fg); clip-path: polygon(45% 62.1%,45% 100%,55% 100%,55% 62.1%,81.8% 88.9%,88.9% 81.8%,62.1% 55%,100% 55%,100% 45%,62.1% 45%,88.9% 18.2%,81.8% 11.1%,55% 37.9%,55% 0%,45% 0%,45% 37.9%,18.2% 11.1%,11.1% 18.2%,37.9% 45%,0% 45%,0% 55%,37.9% 55%,11.1% 81.8%,18.2% 88.9%); }
  h1 { font-size: 20px; font-weight: 600; margin-bottom: 8px; }
  p { font-size: 14px; color: var(--fg2); line-height: 1.5; }
  .hint { margin-top: 24px; font-size: 13px; color: var(--fg2); opacity: 0.7; }
</style>
</head>
<body>
  <div class="card">
    <div class="icon-wrap">
      <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" d="M4.5 12.75l6 6 9-13.5"/></svg>
    </div>
    <div class="brand"><span class="asterisk"></span></div>
    <h1>Authentication successful</h1>
    <p>You can close this tab and return to the terminal.</p>
    <p class="hint">Your CLI session is now authenticated.</p>
  </div>
  <script>setTimeout(function(){window.close()},3000)</script>
</body>
</html>`

func runAuthLogout(cmd *cobra.Command, _ []string) error {
	profile := resolveProfile(cmd)
	cfg, _ := cli.LoadCLIConfigForProfile(profile)
	if cfg.Token == "" && cfg.ServiceTokenKeychainAccount == "" {
		fmt.Fprintln(os.Stderr, "Not authenticated.")
		return nil
	}

	if cfg.ServiceTokenKeychainAccount != "" && runtime.GOOS == "darwin" {
		_ = exec.Command("/usr/bin/security", "delete-generic-password", "-a", cfg.ServiceTokenKeychainAccount, "-s", serviceTokenKeychainService).Run()
	}
	cfg.Token = ""
	cfg.ServiceTokenKeychainAccount = ""
	if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Fprintln(os.Stderr, "Token removed. You are now logged out.")
	return nil
}
