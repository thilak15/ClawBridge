package daemon

import (
	"context"
	"net/http"
	"time"
)

const (
	DefaultTickInterval  = 1 * time.Second
	DefaultJumpThreshold = 5 * time.Second
)

// SleepWakeDetector detects OS sleep/wake cycles by monitoring wall-clock jumps.
type SleepWakeDetector struct {
	tickInterval  time.Duration
	jumpThreshold time.Duration
	onWake        func()
	checkNetwork  func() bool
}

// NewSleepWakeDetector creates a new SleepWakeDetector instance.
func NewSleepWakeDetector(
	tickInterval, jumpThreshold time.Duration,
	onWake func(),
	checkNetwork func() bool,
) *SleepWakeDetector {
	if tickInterval <= 0 {
		tickInterval = DefaultTickInterval
	}
	if jumpThreshold <= 0 {
		jumpThreshold = DefaultJumpThreshold
	}
	if checkNetwork == nil {
		checkNetwork = defaultNetworkCheck
	}
	return &SleepWakeDetector{
		tickInterval:  tickInterval,
		jumpThreshold: jumpThreshold,
		onWake:        onWake,
		checkNetwork:  checkNetwork,
	}
}

func defaultNetworkCheck() bool {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("https://1.1.1.1")
	if err == nil {
		_ = resp.Body.Close()
		return true
	}
	return false
}

// CheckJump returns true if the elapsed duration indicates an OS sleep/wake jump.
func (d *SleepWakeDetector) CheckJump(elapsed time.Duration) bool {
	return elapsed > (d.tickInterval + d.jumpThreshold)
}

// Start begins the sleep/wake monitoring loop.
func (d *SleepWakeDetector) Start(ctx context.Context) {
	ticker := time.NewTicker(d.tickInterval)
	lastTime := time.Now()

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				elapsed := now.Sub(lastTime)
				if d.CheckJump(elapsed) {
					// Sleep/wake jump detected
					if d.checkNetwork != nil {
						// Allow network stack brief moment to stabilize
						for attempt := 0; attempt < 3; attempt++ {
							if d.checkNetwork() {
								break
							}
							time.Sleep(500 * time.Millisecond)
						}
					}
					if d.onWake != nil {
						d.onWake()
					}
				}
				lastTime = now
			}
		}
	}()
}
