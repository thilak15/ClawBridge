package cli

import (
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
	"clawbridge/pkg/bridge"
	"clawbridge/pkg/daemon"
	"clawbridge/pkg/dispatch"
	"clawbridge/pkg/inbound"
	"clawbridge/pkg/mcp"
	"clawbridge/pkg/runner"
	"clawbridge/pkg/vault"
	"clawbridge/pkg/wizard"
)

const (
	Version = "1.0.1"
	Banner  = `=======================================================
                      ClawBridge                       
     Zero-Trust Universal Connector & MCP Gateway      
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

	// 5. Inbound broadcaster & MCP tools
	broadcaster := inbound.NewEventBroadcaster()
	limiter := dispatch.NewTokenBucketLimiter(nil)
	dispatcher := dispatch.NewEgressDispatcher(credVault, limiter, nil)
	tools := mcp.NewTools(dispatcher, nil, nil)

	return &AppContext{
		BaseDir:     baseDir,
		DBMgr:       dbMgr,
		Vault:       credVault,
		Registry:    registry,
		AgentsDir:   agentsDir,
		Broadcaster: broadcaster,
		Tools:       tools,
	}, nil
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

	case "bridge":
		return runBridgeSubcommand(args[1:], in, out, errOut)

	case "agent":
		return runAgentSubcommand(args[1:], in, out, errOut)

	case "mcp":
		return runMCPServer(in, out, errOut)

	case "service":
		return runServiceSubcommand(args[1:], in, out, errOut)

	case "start":
		return runStartDaemon(args[1:], in, out, errOut)

	case "approvals":
		return runApprovalsSubcommand(args[1:], in, out, errOut)

	case "approve":
		return runResolveApproval(args[1:], "approved", out, errOut)

	case "reject":
		return runResolveApproval(args[1:], "rejected", out, errOut)

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
	fmt.Fprintln(out, "  clawbridge connect [app]         Connect Slack, Jira, GitHub, Discord, Twitter, Google, Meta")
	fmt.Fprintln(out, "  clawbridge mcp                   Expose stdio Model Context Protocol (MCP) server")
	fmt.Fprintln(out, "  clawbridge bridge openclaw       Configure official OpenClaw MCP integration (~/.openclaw)")
	fmt.Fprintln(out, "  clawbridge bridge nemoclaw       Configure official NVIDIA NemoClaw sandbox (~/.nemoclaw)")
	fmt.Fprintln(out, "  clawbridge run openclaw [args]   Launch official OpenClaw with ClawBridge MCP tools")
	fmt.Fprintln(out, "  clawbridge run nemoclaw [args]   Launch official NVIDIA NemoClaw / OpenShell runtime")
	fmt.Fprintln(out, "  clawbridge run [--all | agents]  Run agents in interactive console")
	fmt.Fprintln(out, "  clawbridge agent create <name>   Create a new autonomous agent configuration")
	fmt.Fprintln(out, "  clawbridge agent edit <name>     Edit an agent configuration in $EDITOR")
	fmt.Fprintln(out, "  clawbridge config                Configure per-agent access permissions")
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

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	daemon.StartWALCheckpointer(ctx, app.DBMgr, 15*time.Minute)

	fmt.Fprintln(out, Banner)
	fmt.Fprintln(out, "")

	err = runner.RunConsoleWithConfig(ctx, runner.ConsoleConfig{
		In:          in,
		Out:         out,
		Broadcaster: app.Broadcaster,
		Vault:       app.Vault,
		Registry:    app.Registry,
		AgentsDir:   app.AgentsDir,
		Tools:       app.Tools,
	})
	if err != nil {
		fmt.Fprintf(errOut, "Console exited: %v\n", err)
		return 1
	}
	return 0
}

func runAgents(agentNames []string, in io.Reader, out io.Writer, errOut io.Writer) int {
	if len(agentNames) > 0 {
		target := strings.ToLower(agentNames[0])
		if target == "openclaw" || target == "--openclaw" {
			return runOpenClaw(agentNames[1:], in, out, errOut)
		}
		if target == "nemoclaw" || target == "--nemoclaw" {
			return runNemoClaw(agentNames[1:], in, out, errOut)
		}
	}

	return runDefaultOrQuickstart(in, out, errOut)
}

func runOpenClaw(args []string, in io.Reader, out io.Writer, errOut io.Writer) int {
	app, err := InitAppContext("")
	if err != nil {
		fmt.Fprintf(errOut, "Initialization error: %v\n", err)
		return 1
	}
	defer app.DBMgr.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	envVars := collectVaultEnv(app.Vault)
	selfBin, _ := os.Executable()

	if err := bridge.RunOpenClaw(ctx, "", selfBin, args, envVars, in, out, errOut); err != nil {
		fmt.Fprintf(errOut, "OpenClaw runner error: %v\n", err)
		return 1
	}
	return 0
}

func runNemoClaw(args []string, in io.Reader, out io.Writer, errOut io.Writer) int {
	app, err := InitAppContext("")
	if err != nil {
		fmt.Fprintf(errOut, "Initialization error: %v\n", err)
		return 1
	}
	defer app.DBMgr.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	envVars := collectVaultEnv(app.Vault)
	selfBin, _ := os.Executable()

	if err := bridge.RunNemoClaw(ctx, "", selfBin, args, envVars, in, out, errOut); err != nil {
		fmt.Fprintf(errOut, "NemoClaw runner error: %v\n", err)
		return 1
	}
	return 0
}

func collectVaultEnv(v *vault.CredentialVault) map[string]string {
	env := make(map[string]string)
	if v == nil {
		return env
	}
	for _, provider := range []string{"gemini", "openai", "anthropic"} {
		if cred, err := v.GetCredentialByChannel(provider); err == nil && cred != nil {
			if k, ok := cred.Data["key"].(string); ok && k != "" {
				env[strings.ToUpper(provider)+"_API_KEY"] = k
			}
		}
	}
	return env
}

func runBridgeSubcommand(args []string, in io.Reader, out io.Writer, errOut io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(errOut, "Usage: clawbridge bridge <openclaw|nemoclaw>")
		return 1
	}
	target := strings.ToLower(args[0])
	selfBin, _ := os.Executable()
	switch target {
	case "openclaw":
		cfgFile, err := bridge.ConfigureOpenClawBridge("", selfBin)
		if err != nil {
			fmt.Fprintf(errOut, "Failed to configure OpenClaw bridge: %v\n", err)
			return 1
		}
		fmt.Fprintf(out, "Successfully configured OpenClaw MCP integration at '%s'.\n", cfgFile)
		fmt.Fprintln(out, "Official OpenClaw can now access all ClawBridge connectors via MCP.")
		return 0
	case "nemoclaw":
		policyFile, err := bridge.ConfigureNemoClawBridge("", selfBin)
		if err != nil {
			fmt.Fprintf(errOut, "Failed to configure NVIDIA NemoClaw bridge: %v\n", err)
			return 1
		}
		fmt.Fprintf(out, "Successfully configured NVIDIA OpenShell sandbox policy at '%s'.\n", policyFile)
		fmt.Fprintln(out, "Official NemoClaw can now access all ClawBridge connectors within its sandbox.")
		return 0
	default:
		fmt.Fprintf(errOut, "Unknown bridge target: '%s'. Supported targets: 'openclaw', 'nemoclaw'.\n", target)
		return 1
	}
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
	if len(args) == 0 {
		fmt.Fprintln(errOut, "Usage: clawbridge agent <create|edit|list> [arguments]")
		return 1
	}

	app, err := InitAppContext("")
	if err != nil {
		fmt.Fprintf(errOut, "Initialization error: %v\n", err)
		return 1
	}
	defer app.DBMgr.Close()

	sub := strings.ToLower(args[0])
	switch sub {
	case "create":
		if len(args) < 2 {
			fmt.Fprintln(errOut, "Usage: clawbridge agent create <name>")
			return 1
		}
		name := strings.TrimSpace(args[1])
		if name == "" {
			fmt.Fprintln(errOut, "Agent name cannot be empty.")
			return 1
		}
		filePath, err := agents.CreateAgentYAML(app.AgentsDir, agents.AgentConfig{
			Name:         name,
			Description:  fmt.Sprintf("Autonomous agent '%s'", name),
			SystemPrompt: fmt.Sprintf("You are %s, an autonomous agent equipped with ClawBridge tools.", name),
			Model:        "gemini-3.8-flash",
			Permissions:  []string{"*"},
		})
		if err != nil {
			fmt.Fprintf(errOut, "Failed to create agent: %v\n", err)
			return 1
		}
		_, _, _ = app.Registry.RegisterAgent(name, []string{"*"})
		fmt.Fprintf(out, "Created agent configuration at '%s'. Full access permissions granted (*).\n", filePath)
		fmt.Fprintf(out, "Edit anytime with: clawbridge agent edit %s\n", name)
		return 0

	case "edit":
		if len(args) < 2 {
			fmt.Fprintln(errOut, "Usage: clawbridge agent edit <name>")
			return 1
		}
		name := strings.TrimSpace(args[1])
		filePath := filepath.Join(app.AgentsDir, fmt.Sprintf("%s.yaml", name))
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			fmt.Fprintf(errOut, "Agent configuration file '%s' does not exist.\n", filePath)
			return 1
		}
		editor := os.Getenv("EDITOR")
		if editor == "" {
			for _, e := range []string{"code", "nano", "vim", "vi"} {
				if p, err := exec.LookPath(e); err == nil {
					editor = p
					break
				}
			}
		}
		if editor == "" {
			fmt.Fprintf(out, "Agent configuration file located at:\n  %s\n", filePath)
			return 0
		}
		cmd := exec.Command(editor, filePath)
		cmd.Stdin = in
		cmd.Stdout = out
		cmd.Stderr = errOut
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(errOut, "Editor error: %v\n", err)
			return 1
		}
		return 0

	case "list":
		entries, err := os.ReadDir(app.AgentsDir)
		if err != nil {
			fmt.Fprintf(errOut, "Failed to read agents directory: %v\n", err)
			return 1
		}
		fmt.Fprintf(out, "Configured agents in '%s':\n", app.AgentsDir)
		for _, e := range entries {
			if !e.IsDir() && (strings.HasSuffix(e.Name(), ".yaml") || strings.HasSuffix(e.Name(), ".yml")) {
				fmt.Fprintf(out, "  - %s\n", strings.TrimSuffix(strings.TrimSuffix(e.Name(), ".yaml"), ".yml"))
			}
		}
		return 0

	default:
		fmt.Fprintf(errOut, "Unknown agent action: '%s'. Use 'create', 'edit', or 'list'.\n", sub)
		return 1
	}
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

	daemon.StartWALCheckpointer(ctx, app.DBMgr, 15*time.Minute)
	refresher := daemon.NewTokenRefresher(app.Vault, 10*time.Minute, 5*time.Minute, nil)
	refresher.Start(ctx)

	fmt.Fprintln(out, "ClawBridge daemon active (WAL checkpointer and OAuth refresher running).")

	// Keep running until signal
	<-ctx.Done()
	fmt.Fprintln(out, "ClawBridge daemon stopped gracefully.")
	return 0
}

func runApprovalsSubcommand(args []string, in io.Reader, out, errOut io.Writer) int {
	app, err := InitAppContext("")
	if err != nil {
		fmt.Fprintf(errOut, "Failed to initialize: %v\n", err)
		return 1
	}
	defer app.DBMgr.Close()

	status := "pending"
	if len(args) > 0 && args[0] != "" {
		status = args[0]
		if status == "all" {
			status = ""
		}
	}

	list, err := app.DBMgr.ListPendingApprovals(status)
	if err != nil {
		fmt.Fprintf(errOut, "Failed to list approvals: %v\n", err)
		return 1
	}

	if len(list) == 0 {
		fmt.Fprintf(out, "No approvals found with status '%s'.\n", status)
		return 0
	}

	fmt.Fprintf(out, "Approvals (%d):\n", len(list))
	for _, a := range list {
		fmt.Fprintf(out, "  ID: %s | Agent: %s | Tool: %s | Status: %s | Created: %s\n",
			a.ID, a.AgentName, a.ToolName, a.Status, a.CreatedAt.Format("2006-01-02 15:04:05"))
		fmt.Fprintf(out, "     Payload: %s\n", a.ArgumentsJSON)
	}
	return 0
}

func runResolveApproval(args []string, status string, out, errOut io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintf(errOut, "Usage: clawbridge %s <approval_id>\n", status[:len(status)-1])
		return 1
	}
	approvalID := args[0]

	app, err := InitAppContext("")
	if err != nil {
		fmt.Fprintf(errOut, "Failed to initialize: %v\n", err)
		return 1
	}
	defer app.DBMgr.Close()

	if err := app.DBMgr.ResolveApproval(approvalID, status); err != nil {
		fmt.Fprintf(errOut, "Failed to update approval '%s': %v\n", approvalID, err)
		return 1
	}

	fmt.Fprintf(out, "Approval '%s' has been %s.\n", approvalID, status)
	return 0
}
