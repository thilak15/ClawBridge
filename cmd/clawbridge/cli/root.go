package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"clawbridge/pkg/agents"
	"clawbridge/pkg/auth"
	"clawbridge/pkg/daemon"
	"clawbridge/pkg/dispatch"
	"clawbridge/pkg/inbound"
	"clawbridge/pkg/llm"
	"clawbridge/pkg/mcp"
	"clawbridge/pkg/runner"
	"clawbridge/pkg/vault"
	"clawbridge/pkg/wizard"
)

const (
	Version = "1.0.1"
	Banner  = `=======================================================
                      ClawBridge                       
         Zero-Trust Multi-Agent Egress Gateway         
=======================================================`
)

type AppContext struct {
	BaseDir     string
	DBMgr       *vault.DatabaseManager
	Vault       *vault.CredentialVault
	Registry    *auth.AgentRegistry
	AgentsDir   string
	Broadcaster *inbound.EventBroadcaster
	Tools       *mcp.Tools
	LLMClient   *llm.Client
}

func getBaseDir() string {
	if dir := os.Getenv("CLAWBRIDGE_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".clawbridge"
	}
	return filepath.Join(home, ".clawbridge")
}

func InitAppContext(baseDir string) (*AppContext, error) {
	if baseDir == "" {
		baseDir = getBaseDir()
	}
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create base directory: %w", err)
	}

	// 1. Master encryption key
	keyPath := filepath.Join(baseDir, "master.key")
	var key []byte
	if data, err := os.ReadFile(keyPath); err == nil && len(data) == 32 {
		key = data
	} else {
		newKey, err := vault.GenerateKey()
		if err != nil {
			return nil, fmt.Errorf("failed to generate master key: %w", err)
		}
		if err := os.WriteFile(keyPath, newKey, 0600); err != nil {
			return nil, fmt.Errorf("failed to write master key: %w", err)
		}
		key = newKey
	}

	cipher, err := vault.NewEnvelopeCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize cipher: %w", err)
	}

	// 2. Database manager
	dbPath := filepath.Join(baseDir, "vault.db")
	dbMgr, err := vault.NewDatabaseManager(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	if err := dbMgr.Initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize database schema: %w", err)
	}

	credVault := vault.NewCredentialVault(dbMgr, cipher)
	registry := auth.NewAgentRegistry(dbMgr)

	// 3. Auto-sync .env file
	_, _ = vault.AutoSyncEnv(credVault, ".env")

	// Check if vault has LLM credentials if not already set in environment
	if os.Getenv("GEMINI_API_KEY") == "" {
		if cred, err := credVault.GetCredentialByChannel("gemini"); err == nil && cred != nil {
			if k, ok := cred.Data["key"].(string); ok && k != "" {
				_ = os.Setenv("GEMINI_API_KEY", k)
			}
		}
	}
	if os.Getenv("OPENAI_API_KEY") == "" {
		if cred, err := credVault.GetCredentialByChannel("openai"); err == nil && cred != nil {
			if k, ok := cred.Data["key"].(string); ok && k != "" {
				_ = os.Setenv("OPENAI_API_KEY", k)
			}
		}
	}
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		if cred, err := credVault.GetCredentialByChannel("anthropic"); err == nil && cred != nil {
			if k, ok := cred.Data["key"].(string); ok && k != "" {
				_ = os.Setenv("ANTHROPIC_API_KEY", k)
			}
		}
	}

	// 4. Agents directory
	agentsDir := "agents"
	if _, err := os.Stat(agentsDir); os.IsNotExist(err) {
		agentsDir = filepath.Join(baseDir, "agents")
		_ = os.MkdirAll(agentsDir, 0755)
	}

	ensureBuiltinAgents(agentsDir)

	// 5. Inbound broadcaster & MCP tools
	broadcaster := inbound.NewEventBroadcaster()
	limiter := dispatch.NewTokenBucketLimiter(nil)
	dispatcher := dispatch.NewEgressDispatcher(credVault, limiter, nil)
	tools := mcp.NewTools(dispatcher, nil, nil)

	// 6. LLM Client
	llmClient := llm.NewClientFromEnv(nil)

	return &AppContext{
		BaseDir:     baseDir,
		DBMgr:       dbMgr,
		Vault:       credVault,
		Registry:    registry,
		AgentsDir:   agentsDir,
		Broadcaster: broadcaster,
		Tools:       tools,
		LLMClient:   llmClient,
	}, nil
}

func ensureBuiltinAgents(dir string) {
	_ = os.MkdirAll(dir, 0755)
	nemoPath := filepath.Join(dir, "nemoclaw.yaml")
	if _, err := os.Stat(nemoPath); os.IsNotExist(err) {
		_, _ = agents.CreateAgentYAML(dir, agents.AgentConfig{
			Name:         "nemoclaw",
			Description:  "General-purpose autonomous agent with universal tool access",
			SystemPrompt: "You are NemoClaw, an autonomous multi-platform agent equipped with ClawBridge tools.",
			Model:        "gemini-1.5-flash",
			Permissions:  []string{"*"},
		})
	}

	openPath := filepath.Join(dir, "openclaw.yaml")
	if _, err := os.Stat(openPath); os.IsNotExist(err) {
		_, _ = agents.CreateAgentYAML(dir, agents.AgentConfig{
			Name:         "openclaw",
			Description:  "Autonomous agent with universal access for engineering tasks",
			SystemPrompt: "You are OpenClaw, an autonomous multi-platform agent equipped with ClawBridge tools.",
			Model:        "gemini-1.5-flash",
			Permissions:  []string{"*"},
		})
	}
}

// Execute parses command-line arguments and dispatches to appropriate subcommand.
func Execute(args []string, in io.Reader, out io.Writer, errOut io.Writer) int {
	if len(args) == 0 {
		return runDefaultOrQuickstart(in, out, errOut)
	}

	cmd := strings.ToLower(args[0])
	switch cmd {
	case "help", "--help", "-h":
		printHelp(out)
		return 0

	case "version", "--version", "-v":
		fmt.Fprintf(out, "ClawBridge v%s (pure Go, zero CGO)\n", Version)
		return 0

	case "run":
		return runAgents(args[1:], in, out, errOut)

	case "connect":
		return runConnect(args[1:], in, out, errOut)

	case "config":
		return runConfig(args[1:], in, out, errOut)

	case "agent":
		return runAgentSubcommand(args[1:], in, out, errOut)

	case "mcp":
		return runMCPServer(in, out, errOut)

	case "service":
		return runServiceSubcommand(args[1:], in, out, errOut)

	case "start":
		return runStartDaemon(args[1:], in, out, errOut)

	default:
		fmt.Fprintf(errOut, "Unknown command: '%s'. Type 'clawbridge help' for usage.\n", cmd)
		return 1
	}
}

func printHelp(out io.Writer) {
	fmt.Fprintln(out, Banner)
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Usage: clawbridge <command> [arguments]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Core Commands:")
	fmt.Fprintln(out, "  clawbridge                       Run 30-second quickstart or launch console")
	fmt.Fprintln(out, "  clawbridge run [--all | agents]  Run agents concurrently in interactive console")
	fmt.Fprintln(out, "  clawbridge connect [app]         Connect Slack, Jira, Discord, Twitter, GitHub")
	fmt.Fprintln(out, "  clawbridge config                Configure per-agent access permissions")
	fmt.Fprintln(out, "  clawbridge agent create <name>   Create a new autonomous agent configuration")
	fmt.Fprintln(out, "  clawbridge mcp                   Expose stdio Model Context Protocol (MCP) server")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "24/7 Daemon Service Commands:")
	fmt.Fprintln(out, "  clawbridge service install       Install background OS service (launchd/systemd)")
	fmt.Fprintln(out, "  clawbridge service uninstall     Remove background OS service")
	fmt.Fprintln(out, "  clawbridge service start         Start background service")
	fmt.Fprintln(out, "  clawbridge service stop          Stop background service")
	fmt.Fprintln(out, "  clawbridge service status        Inspect status of background daemon")
	fmt.Fprintln(out, "  clawbridge service logs          Tail background daemon logs")
	fmt.Fprintln(out, "  clawbridge start -d              Run detached in background with PID tracking")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Flags:")
	fmt.Fprintln(out, "  -h, --help                       Show help guidance")
	fmt.Fprintln(out, "  -v, --version                    Show version information")
}

func runDefaultOrQuickstart(in io.Reader, out io.Writer, errOut io.Writer) int {
	app, err := InitAppContext("")
	if err != nil {
		fmt.Fprintf(errOut, "Initialization error: %v\n", err)
		return 1
	}
	defer app.DBMgr.Close()

	// Check if any app credentials exist
	hasCreds := false
	for _, ch := range []string{"slack", "jira", "discord", "twitter", "github"} {
		if cred, err := app.Vault.GetCredentialByChannel(ch); err == nil && cred != nil {
			hasCreds = true
			break
		}
	}

	// Check if any LLM credentials exist
	hasLLM := os.Getenv("GEMINI_API_KEY") != "" || os.Getenv("GOOGLE_API_KEY") != "" ||
		os.Getenv("OPENAI_API_KEY") != "" || os.Getenv("ANTHROPIC_API_KEY") != "" ||
		os.Getenv("OLLAMA_HOST") != ""
	if !hasLLM {
		for _, ch := range []string{"gemini", "openai", "anthropic"} {
			if cred, err := app.Vault.GetCredentialByChannel(ch); err == nil && cred != nil {
				hasLLM = true
				break
			}
		}
	}

	scanner := bufio.NewScanner(in)
	prompt := func(msg string) string {
		fmt.Fprint(out, msg)
		if !scanner.Scan() {
			return ""
		}
		return strings.TrimSpace(scanner.Text())
	}

	// If no LLM credentials configured, prompt user to select their LLM provider
	if !hasLLM {
		fmt.Fprintln(out, Banner)
		fmt.Fprintln(out, "\nWelcome to ClawBridge! Let's get your agents set up in 30 seconds.")
		fmt.Fprintln(out, "\nStep 1: Choose your LLM Provider:")
		fmt.Fprintln(out, "  1) Google Gemini (Recommended - free tier at aistudio.google.com)")
		fmt.Fprintln(out, "  2) OpenAI (GPT-4o / GPT-4o-mini)")
		fmt.Fprintln(out, "  3) Anthropic Claude (Claude 3.5 Haiku / Sonnet)")
		fmt.Fprintln(out, "  4) Local Ollama (offline local model)")
		fmt.Fprintln(out, "  5) Skip / Offline mode")
		fmt.Fprintln(out, "")

		choice := prompt("Select provider [1-5, default 1]: ")
		if choice == "" {
			choice = "1"
		}

		// Check if user directly pasted an API key
		detected := llm.DetectProviderFromKey(choice)
		if detected != "" {
			switch detected {
			case llm.ProviderGemini:
				_ = os.Setenv("GEMINI_API_KEY", choice)
				_ = vault.SaveEnvVar(".env", "GEMINI_API_KEY", choice)
				_, _ = app.Vault.UpsertCredentialByChannel("gemini", "api_key", map[string]any{"key": choice})
				fmt.Fprintln(out, "Detected Google Gemini key. Configured successfully.")
			case llm.ProviderOpenAI:
				_ = os.Setenv("OPENAI_API_KEY", choice)
				_ = vault.SaveEnvVar(".env", "OPENAI_API_KEY", choice)
				_, _ = app.Vault.UpsertCredentialByChannel("openai", "api_key", map[string]any{"key": choice})
				fmt.Fprintln(out, "Detected OpenAI key. Configured successfully.")
			case llm.ProviderAnthropic:
				_ = os.Setenv("ANTHROPIC_API_KEY", choice)
				_ = vault.SaveEnvVar(".env", "ANTHROPIC_API_KEY", choice)
				_, _ = app.Vault.UpsertCredentialByChannel("anthropic", "api_key", map[string]any{"key": choice})
				fmt.Fprintln(out, "Detected Anthropic key. Configured successfully.")
			}
			app.LLMClient = llm.NewClientFromEnv(nil)
		} else {
			switch choice {
			case "1", "gemini", "google":
				key := prompt("\nEnter Google Gemini API Key (starts with AIza... or AQ...): ")
				if key != "" {
					_ = os.Setenv("GEMINI_API_KEY", key)
					_ = vault.SaveEnvVar(".env", "GEMINI_API_KEY", key)
					_, _ = app.Vault.UpsertCredentialByChannel("gemini", "api_key", map[string]any{"key": key})
					app.LLMClient = llm.NewClientFromEnv(nil)
					fmt.Fprintln(out, "Configured Google Gemini successfully.")
				}
			case "2", "openai":
				key := prompt("\nEnter OpenAI API Key (starts with sk-...): ")
				if key != "" {
					_ = os.Setenv("OPENAI_API_KEY", key)
					_ = vault.SaveEnvVar(".env", "OPENAI_API_KEY", key)
					_, _ = app.Vault.UpsertCredentialByChannel("openai", "api_key", map[string]any{"key": key})
					app.LLMClient = llm.NewClientFromEnv(nil)
					fmt.Fprintln(out, "Configured OpenAI successfully.")
				}
			case "3", "anthropic", "claude":
				key := prompt("\nEnter Anthropic API Key (starts with sk-ant-...): ")
				if key != "" {
					_ = os.Setenv("ANTHROPIC_API_KEY", key)
					_ = vault.SaveEnvVar(".env", "ANTHROPIC_API_KEY", key)
					_, _ = app.Vault.UpsertCredentialByChannel("anthropic", "api_key", map[string]any{"key": key})
					app.LLMClient = llm.NewClientFromEnv(nil)
					fmt.Fprintln(out, "Configured Anthropic Claude successfully.")
				}
			case "4", "ollama":
				host := prompt("\nEnter Ollama Host URL [default: http://localhost:11434]: ")
				if host == "" {
					host = "http://localhost:11434"
				}
				_ = os.Setenv("OLLAMA_HOST", host)
				_ = vault.SaveEnvVar(".env", "OLLAMA_HOST", host)
				_, _ = app.Vault.UpsertCredentialByChannel("ollama", "host", map[string]any{"url": host})
				app.LLMClient = llm.NewClientFromEnv(nil)
				fmt.Fprintln(out, "Configured Local Ollama successfully.")
			default:
				fmt.Fprintln(out, "Running in offline mode (LLM disabled).")
			}
		}
	}

	// If no app credentials, prompt to connect apps
	if !hasCreds {
		wantConnect := prompt("\nWould you like to connect an app (Slack, Jira, Discord, Twitter, GitHub)? [y/N]: ")
		if strings.EqualFold(wantConnect, "y") || strings.EqualFold(wantConnect, "yes") {
			_ = wizard.RunConnectWizard(in, out, app.Vault, "")
		}
	}

	return runOrchestratorConsole(app, []string{"--all"}, in, out, errOut)
}

func runAgents(agentNames []string, in io.Reader, out io.Writer, errOut io.Writer) int {
	app, err := InitAppContext("")
	if err != nil {
		fmt.Fprintf(errOut, "Initialization error: %v\n", err)
		return 1
	}
	defer app.DBMgr.Close()

	return runOrchestratorConsole(app, agentNames, in, out, errOut)
}

func runOrchestratorConsole(app *AppContext, agentNames []string, in io.Reader, out io.Writer, errOut io.Writer) int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Start supervisor & WAL checkpointer
	daemon.StartWALCheckpointer(ctx, app.DBMgr, 15*time.Minute)

	orch := runner.NewOrchestrator(app.AgentsDir, app.Registry, app.LLMClient, app.Tools, app.Broadcaster, out)

	// Determine agents to start
	if len(agentNames) == 0 || (len(agentNames) == 1 && agentNames[0] == "--all") {
		started, err := orch.StartAll(ctx)
		if err != nil {
			fmt.Fprintf(errOut, "Failed to start agents: %v\n", err)
			return 1
		}
		fmt.Fprintf(out, "Started agents (%d): %s\n", len(started), strings.Join(started, ", "))
	} else {
		for _, name := range agentNames {
			if strings.TrimSpace(name) == "" || strings.HasPrefix(name, "-") {
				continue
			}
			if err := orch.StartAgent(ctx, name); err != nil {
				fmt.Fprintf(errOut, "Failed to start agent '%s': %v\n", name, err)
			}
		}
	}

	// Launch console REPL
	err := runner.RunConsole(ctx, in, out, orch, app.Broadcaster)
	orch.StopAll()
	if err != nil {
		fmt.Fprintf(errOut, "Console exited: %v\n", err)
		return 1
	}

	return 0
}

func runConnect(args []string, in io.Reader, out io.Writer, errOut io.Writer) int {
	app, err := InitAppContext("")
	if err != nil {
		fmt.Fprintf(errOut, "Initialization error: %v\n", err)
		return 1
	}
	defer app.DBMgr.Close()

	targetApp := ""
	if len(args) > 0 {
		targetApp = args[0]
	}

	if err := wizard.RunConnectWizard(in, out, app.Vault, targetApp); err != nil {
		fmt.Fprintf(errOut, "Connect wizard error: %v\n", err)
		return 1
	}
	return 0
}

func runConfig(args []string, in io.Reader, out io.Writer, errOut io.Writer) int {
	app, err := InitAppContext("")
	if err != nil {
		fmt.Fprintf(errOut, "Initialization error: %v\n", err)
		return 1
	}
	defer app.DBMgr.Close()

	if err := wizard.RunConfigWizard(in, out, app.Registry, app.AgentsDir); err != nil {
		fmt.Fprintf(errOut, "Config wizard error: %v\n", err)
		return 1
	}
	return 0
}

func runAgentSubcommand(args []string, in io.Reader, out io.Writer, errOut io.Writer) int {
	if len(args) < 2 || strings.ToLower(args[0]) != "create" {
		fmt.Fprintln(errOut, "Usage: clawbridge agent create <name>")
		return 1
	}

	name := strings.TrimSpace(args[1])
	if name == "" {
		fmt.Fprintln(errOut, "Agent name cannot be empty.")
		return 1
	}

	app, err := InitAppContext("")
	if err != nil {
		fmt.Fprintf(errOut, "Initialization error: %v\n", err)
		return 1
	}
	defer app.DBMgr.Close()

	filePath, err := agents.CreateAgentYAML(app.AgentsDir, agents.AgentConfig{
		Name:         name,
		Description:  fmt.Sprintf("Autonomous agent '%s'", name),
		SystemPrompt: fmt.Sprintf("You are %s, an autonomous agent equipped with ClawBridge tools.", name),
		Model:        "gemini-1.5-flash",
		Permissions:  []string{"*"},
	})
	if err != nil {
		fmt.Fprintf(errOut, "Failed to create agent: %v\n", err)
		return 1
	}

	_, _, _ = app.Registry.RegisterAgent(name, []string{"*"})
	fmt.Fprintf(out, "Created agent configuration at '%s'. Full access permissions granted (*).\n", filePath)
	return 0
}

func runMCPServer(in io.Reader, out io.Writer, errOut io.Writer) int {
	app, err := InitAppContext("")
	if err != nil {
		fmt.Fprintf(errOut, "Initialization error: %v\n", err)
		return 1
	}
	defer app.DBMgr.Close()

	server := mcp.NewServer("ClawBridge", Version, app.Tools)
	if err := server.ServeStdio(context.Background(), in, out); err != nil {
		fmt.Fprintf(errOut, "MCP server exited: %v\n", err)
		return 1
	}
	return 0
}

func runServiceSubcommand(args []string, in io.Reader, out io.Writer, errOut io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(errOut, "Usage: clawbridge service [install | uninstall | start | stop | status | logs]")
		return 1
	}

	action := strings.ToLower(args[0])
	binPath, err := os.Executable()
	if err != nil {
		binPath = "clawbridge"
	}

	baseDir := getBaseDir()
	logPath := filepath.Join(baseDir, "daemon.log")
	pidPath := filepath.Join(baseDir, "daemon.pid")

	switch action {
	case "install":
		if err := daemon.InstallService(binPath, []string{"run", "--all"}, logPath, baseDir); err != nil {
			fmt.Fprintf(errOut, "Failed to install service: %v\n", err)
			return 1
		}
		fmt.Fprintln(out, "Service installed successfully. Run 'clawbridge service start' to begin.")
		return 0

	case "uninstall":
		if err := daemon.UninstallService(); err != nil {
			fmt.Fprintf(errOut, "Failed to uninstall service: %v\n", err)
			return 1
		}
		fmt.Fprintln(out, "Service uninstalled successfully.")
		return 0

	case "start":
		_ = exec.Command("launchctl", "start", daemon.LaunchdLabel).Run()
		_ = exec.Command("systemctl", "--user", "start", "clawbridge.service").Run()
		fmt.Fprintln(out, "Service start command issued.")
		return 0

	case "stop":
		_ = exec.Command("launchctl", "stop", daemon.LaunchdLabel).Run()
		_ = exec.Command("systemctl", "--user", "stop", "clawbridge.service").Run()
		fmt.Fprintln(out, "Service stop command issued.")
		return 0

	case "status":
		pid, err := daemon.ReadPID(pidPath)
		if err == nil && daemon.IsProcessRunning(pid) {
			fmt.Fprintf(out, "ClawBridge daemon is running (PID: %d)\n", pid)
		} else {
			fmt.Fprintln(out, "ClawBridge daemon is stopped.")
		}
		return 0

	case "logs":
		data, err := os.ReadFile(logPath)
		if err != nil {
			fmt.Fprintf(errOut, "No logs found at '%s'\n", logPath)
			return 1
		}
		lines := strings.Split(string(data), "\n")
		start := 0
		if len(lines) > 50 {
			start = len(lines) - 50
		}
		for _, l := range lines[start:] {
			fmt.Fprintln(out, l)
		}
		return 0

	default:
		fmt.Fprintf(errOut, "Unknown service action: '%s'\n", action)
		return 1
	}
}

func runStartDaemon(args []string, in io.Reader, out io.Writer, errOut io.Writer) int {
	isDetached := false
	for _, a := range args {
		if a == "-d" || a == "--daemon" {
			isDetached = true
		}
	}

	baseDir := getBaseDir()
	pidPath := filepath.Join(baseDir, "daemon.pid")

	if isDetached {
		binPath, err := os.Executable()
		if err != nil {
			binPath = "clawbridge"
		}

		cmd := exec.Command(binPath, "start")
		logPath := filepath.Join(baseDir, "daemon.log")
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Fprintf(errOut, "Failed to open daemon log: %v\n", err)
			return 1
		}
		cmd.Stdout = logFile
		cmd.Stderr = logFile

		if err := cmd.Start(); err != nil {
			fmt.Fprintf(errOut, "Failed to start detached daemon: %v\n", err)
			return 1
		}

		_ = daemon.WritePID(pidPath, cmd.Process.Pid)
		fmt.Fprintf(out, "ClawBridge daemon started in background (PID: %d). Logs: %s\n", cmd.Process.Pid, logPath)
		return 0
	}

	// Foreground daemon runner
	_ = daemon.WritePID(pidPath, os.Getpid())
	defer daemon.RemovePID(pidPath)

	app, err := InitAppContext(baseDir)
	if err != nil {
		fmt.Fprintf(errOut, "Initialization error: %v\n", err)
		return 1
	}
	defer app.DBMgr.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	orch := runner.NewOrchestrator(app.AgentsDir, app.Registry, app.LLMClient, app.Tools, app.Broadcaster, out)
	started, err := orch.StartAll(ctx)
	if err != nil {
		fmt.Fprintf(errOut, "Failed to start agents: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "ClawBridge daemon active with agents: %v\n", started)

	// Keep running until signal
	<-ctx.Done()
	orch.StopAll()
	fmt.Fprintln(out, "ClawBridge daemon stopped gracefully.")
	return 0
}
