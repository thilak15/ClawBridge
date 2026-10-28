package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

const (
	LaunchdLabel       = "com.clawbridge.daemon"
	DefaultDaemonDir   = ".clawbridge"
	DefaultPIDFileName = "daemon.pid"
)

// GenerateLaunchdPlist produces a launchd property list XML string for macOS.
func GenerateLaunchdPlist(binPath string, args []string, logPath, workingDir string) string {
	var programArgs strings.Builder
	programArgs.WriteString(fmt.Sprintf("\t\t<string>%s</string>\n", binPath))
	for _, a := range args {
		programArgs.WriteString(fmt.Sprintf("\t\t<string>%s</string>\n", a))
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
%s	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>WorkingDirectory</key>
	<string>%s</string>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`, LaunchdLabel, programArgs.String(), workingDir, logPath, logPath)
}

// GenerateSystemdService produces a systemd unit configuration string for Linux.
func GenerateSystemdService(binPath string, args []string, logPath, workingDir string) string {
	execCmd := binPath
	if len(args) > 0 {
		execCmd = fmt.Sprintf("%s %s", binPath, strings.Join(args, " "))
	}

	return fmt.Sprintf(`[Unit]
Description=ClawBridge 24/7 Agent Daemon
After=network.target

[Service]
Type=simple
WorkingDirectory=%s
ExecStart=%s
Restart=always
RestartSec=5
StandardOutput=append:%s
StandardError=append:%s

[Install]
WantedBy=default.target
`, workingDir, execCmd, logPath, logPath)
}

// WritePID saves process ID to the PID file.
func WritePID(pidPath string, pid int) error {
	dir := filepath.Dir(pidPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0644)
}

// ReadPID reads process ID from the PID file.
func ReadPID(pidPath string) (int, error) {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid pid format: %w", err)
	}
	return pid, nil
}

// RemovePID deletes the PID file.
func RemovePID(pidPath string) error {
	return os.Remove(pidPath)
}

// IsProcessRunning checks if a PID is alive.
func IsProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// InstallService configures launchd on macOS or systemd on Linux.
func InstallService(binPath string, args []string, logPath, workingDir string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home dir: %w", err)
	}

	if workingDir == "" {
		workingDir = home
	}
	if logPath == "" {
		logPath = filepath.Join(home, DefaultDaemonDir, "daemon.log")
	}

	switch runtime.GOOS {
	case "darwin":
		agentsDir := filepath.Join(home, "Library", "LaunchAgents")
		if err := os.MkdirAll(agentsDir, 0755); err != nil {
			return err
		}
		plistPath := filepath.Join(agentsDir, fmt.Sprintf("%s.plist", LaunchdLabel))
		content := GenerateLaunchdPlist(binPath, args, logPath, workingDir)
		return os.WriteFile(plistPath, []byte(content), 0644)

	case "linux":
		userSystemdDir := filepath.Join(home, ".config", "systemd", "user")
		if err := os.MkdirAll(userSystemdDir, 0755); err != nil {
			return err
		}
		servicePath := filepath.Join(userSystemdDir, "clawbridge.service")
		content := GenerateSystemdService(binPath, args, logPath, workingDir)
		return os.WriteFile(servicePath, []byte(content), 0644)

	default:
		return fmt.Errorf("unsupported operating system for service installation: %s", runtime.GOOS)
	}
}

// UninstallService removes launchd or systemd service configurations.
func UninstallService() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	switch runtime.GOOS {
	case "darwin":
		plistPath := filepath.Join(home, "Library", "LaunchAgents", fmt.Sprintf("%s.plist", LaunchdLabel))
		_ = exec.Command("launchctl", "unload", plistPath).Run()
		return os.Remove(plistPath)

	case "linux":
		servicePath := filepath.Join(home, ".config", "systemd", "user", "clawbridge.service")
		_ = exec.Command("systemctl", "--user", "stop", "clawbridge.service").Run()
		_ = exec.Command("systemctl", "--user", "disable", "clawbridge.service").Run()
		return os.Remove(servicePath)

	default:
		return fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}
}
