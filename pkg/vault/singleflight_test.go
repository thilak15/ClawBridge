package vault_test

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"clawbridge/pkg/vault"
)

func TestSingleFlight_DeduplicatesConcurrentCalls(t *testing.T) {
	group := vault.NewSingleFlightGroup()
	var callCount int64

	fn := func() (any, error) {
		atomic.AddInt64(&callCount, 1)
		time.Sleep(50 * time.Millisecond) // simulate OAuth refresh network roundtrip
		return "fresh-access-token-123", nil
	}

	const callers = 25
	var wg sync.WaitGroup
	wg.Add(callers)

	results := make([]any, callers)
	errs := make([]error, callers)

	for i := 0; i < callers; i++ {
		go func(idx int) {
			defer wg.Done()
			res, err := group.Execute("slack-oauth-token", fn)
			results[idx] = res
			errs[idx] = err
		}(i)
	}

	wg.Wait()

	if count := atomic.LoadInt64(&callCount); count != 1 {
		t.Fatalf("expected function to be called exactly 1 time, called %d times", count)
	}

	for i := 0; i < callers; i++ {
		if errs[i] != nil {
			t.Fatalf("caller %d got error: %v", i, errs[i])
		}
		if results[i] != "fresh-access-token-123" {
			t.Fatalf("caller %d expected token 'fresh-access-token-123', got '%v'", i, results[i])
		}
	}
}

func TestSingleFlight_PropagatesError(t *testing.T) {
	group := vault.NewSingleFlightGroup()
	var callCount int64
	expectedErr := errors.New("upstream oauth provider 500 error")

	fn := func() (any, error) {
		atomic.AddInt64(&callCount, 1)
		time.Sleep(20 * time.Millisecond)
		return nil, expectedErr
	}

	const callers = 10
	var wg sync.WaitGroup
	wg.Add(callers)

	errs := make([]error, callers)
	for i := 0; i < callers; i++ {
		go func(idx int) {
			defer wg.Done()
			_, err := group.Execute("failing-oauth-key", fn)
			errs[idx] = err
		}(i)
	}

	wg.Wait()

	if count := atomic.LoadInt64(&callCount); count != 1 {
		t.Fatalf("expected function called once, got %d", count)
	}

	for i := 0; i < callers; i++ {
		if !errors.Is(errs[i], expectedErr) && errs[i].Error() != expectedErr.Error() {
			t.Fatalf("caller %d expected error %v, got %v", i, expectedErr, errs[i])
		}
	}
}
