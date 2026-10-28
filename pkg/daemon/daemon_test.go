package daemon_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"clawbridge/pkg/daemon"
	"clawbridge/pkg/vault"
)

func TestRotatingLogger_RotatesAtMaxBytes(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "test.log")

	// Limit to 50 bytes per file, max 2 backups
	logger, err := daemon.NewRotatingLogger(logPath, 50, 2)
	if err != nil {
		t.Fatalf("failed to create rotating logger: %v", err)
	}
	defer logger.Close()

	// Write 150 bytes in chunks of 30 bytes
	chunk := []byte("123456789012345678901234567890\n") // 31 bytes
	for i := 0; i < 5; i++ {
		if _, err := logger.Write(chunk); err != nil {
			t.Fatalf("write failed on step %d: %v", i, err)
		}
	}

	// Verify base log exists
	if _, err := os.Stat(logPath); err != nil {
		t.Errorf("base log file missing: %v", err)
	}

	// Verify backup .1 exists
	backup1 := fmt.Sprintf("%s.1", logPath)
	if _, err := os.Stat(backup1); err != nil {
		t.Errorf("backup .1 missing: %v", err)
	}

	// Verify backup .2 exists
	backup2 := fmt.Sprintf("%s.2", logPath)
	if _, err := os.Stat(backup2); err != nil {
		t.Errorf("backup .2 missing: %v", err)
	}

	// Verify backup .3 does NOT exist (max backups is 2)
	backup3 := fmt.Sprintf("%s.3", logPath)
	if _, err := os.Stat(backup3); !os.IsNotExist(err) {
		t.Errorf("expected backup .3 to be removed, but it exists")
	}
}

func TestSupervisor_CatchesPanicAndRestarts(t *testing.T) {
	var runCount int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logMessages := make(chan string, 10)
	sup := daemon.NewSupervisor(10*time.Millisecond, func(format string, args ...any) {
		select {
		case logMessages <- fmt.Sprintf(format, args...):
		default:
		}
	})

	workerDone := make(chan struct{})

	go sup.Supervise(ctx, "panic-bot", func(wCtx context.Context) error {
		count := atomic.AddInt32(&runCount, 1)
		if count == 1 {
			panic("simulated fatal error in agent")
		}
		// Second run succeeds: signal and stay running until context canceled
		select {
		case <-workerDone:
		default:
			close(workerDone)
		}
		<-wCtx.Done()
		return nil
	})

	select {
	case <-workerDone:
		// Succeeded after recovering from panic
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for worker to restart after panic")
	}

	cancel() // Stop supervision
	time.Sleep(20 * time.Millisecond)

	if sup.PanicCount("panic-bot") != 1 {
		t.Errorf("expected 1 panic count, got %d", sup.PanicCount("panic-bot"))
	}

	if atomic.LoadInt32(&runCount) < 2 {
		t.Errorf("expected at least 2 runs, got %d", atomic.LoadInt32(&runCount))
	}
}

func TestTokenRefresher_ProactivelyRefreshesExpiring(t *testing.T) {
	tempDir := t.TempDir()
	dbMgr, err := vault.NewDatabaseManager(filepath.Join(tempDir, "vault.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dbMgr.Close()
	_ = dbMgr.Initialize()

	key, _ := vault.GenerateKey()
	cipher, _ := vault.NewEnvelopeCipher(key)
	v := vault.NewCredentialVault(dbMgr, cipher)

	// Token 1: Expires in 60 seconds (within 5m threshold)
	tok1ID, err := v.StoreOAuthToken("slack", "app1", map[string]any{"access_token": "slack_tok_1"}, 60)
	if err != nil {
		t.Fatal(err)
	}

	// Token 2: Expires in 3600 seconds (far away)
	_, err = v.StoreOAuthToken("jira", "app2", map[string]any{"access_token": "jira_tok_2"}, 3600)
	if err != nil {
		t.Fatal(err)
	}

	refreshedIDs := make(map[string]bool)
	refreshFn := func(rec *vault.OAuthTokenRecord) (map[string]any, error) {
		refreshedIDs[rec.ID] = true
		return map[string]any{
			"access_token": "refreshed_" + rec.ID,
			"expires_in":   7200,
		}, nil
	}

	refresher := daemon.NewTokenRefresher(v, 10*time.Minute, 5*time.Minute, refreshFn)

	count, err := refresher.CheckAndRefreshOnce(context.Background())
	if err != nil {
		t.Fatalf("CheckAndRefreshOnce failed: %v", err)
	}

	if count != 1 {
		t.Fatalf("expected 1 token refreshed, got %d", count)
	}

	if !refreshedIDs[tok1ID] {
		t.Errorf("expected token 1 to be refreshed, got %v", refreshedIDs)
	}

	// Verify refreshed token in vault
	freshRec, err := v.GetOAuthToken(tok1ID)
	if err != nil {
		t.Fatal(err)
	}
	if freshRec.TokenData["access_token"] != "refreshed_"+tok1ID {
		t.Errorf("unexpected refreshed token data: %v", freshRec.TokenData)
	}
}

func TestSleepWakeDetector_DetectsJump(t *testing.T) {
	detector := daemon.NewSleepWakeDetector(1*time.Second, 5*time.Second, nil, func() bool { return true })

	// Normal tick duration (1.1s)
	if detector.CheckJump(1100 * time.Millisecond) {
		t.Errorf("did not expect jump detection on normal tick")
	}

	// Sleep jump (15s elapsed between ticks)
	if !detector.CheckJump(15 * time.Second) {
		t.Errorf("expected jump detection on 15s elapsed duration")
	}
}

func TestService_ConfigGenerationAndPID(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Launchd Plist
	plist := daemon.GenerateLaunchdPlist("/usr/local/bin/clawbridge", []string{"run", "--all"}, "/var/log/cb.log", "/opt/cb")
	if !strings.Contains(plist, "com.clawbridge.daemon") {
		t.Errorf("plist missing label")
	}
	if !strings.Contains(plist, "<string>/usr/local/bin/clawbridge</string>") {
		t.Errorf("plist missing binary path")
	}

	// 2. Systemd Service
	systemd := daemon.GenerateSystemdService("/usr/local/bin/clawbridge", []string{"run", "--all"}, "/var/log/cb.log", "/opt/cb")
	if !strings.Contains(systemd, "ExecStart=/usr/local/bin/clawbridge run --all") {
		t.Errorf("systemd missing ExecStart")
	}
	if !strings.Contains(systemd, "Restart=always") {
		t.Errorf("systemd missing Restart=always")
	}

	// 3. PID management
	pidPath := filepath.Join(tempDir, "daemon.pid")
	if err := daemon.WritePID(pidPath, 12345); err != nil {
		t.Fatalf("failed to write pid: %v", err)
	}

	pid, err := daemon.ReadPID(pidPath)
	if err != nil {
		t.Fatalf("failed to read pid: %v", err)
	}
	if pid != 12345 {
		t.Errorf("expected pid 12345, got %d", pid)
	}

	if err := daemon.RemovePID(pidPath); err != nil {
		t.Fatalf("failed to remove pid: %v", err)
	}

	if _, err := daemon.ReadPID(pidPath); err == nil {
		t.Errorf("expected error reading removed pid")
	}
}
