package dispatch_test

import (
	"errors"
	"net/http"
	"testing"

	"clawbridge/pkg/dispatch"
)

func TestErrorNormalizer_NormalizeHTTPResponse_Success(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
	}
	body := []byte(`{"ok": true, "ts": "1700000000.123"}`)

	receipt := dispatch.NormalizeHTTPResponse("slack", resp, body)
	if receipt.Status != "success" {
		t.Fatalf("expected status 'success', got '%s'", receipt.Status)
	}
	if receipt.StatusCode != 200 {
		t.Fatalf("expected status code 200, got %d", receipt.StatusCode)
	}
	if receipt.Code != "" {
		t.Fatalf("expected empty code on success, got '%s'", receipt.Code)
	}
	if receipt.Retryable {
		t.Fatalf("success receipt should not be retryable")
	}
	if receipt.Data == nil {
		t.Fatalf("expected non-nil data payload")
	}
}

func TestErrorNormalizer_NormalizeHTTPResponse_RateLimitedWithRetryAfter(t *testing.T) {
	header := make(http.Header)
	header.Set("Retry-After", "12.5")
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     header,
	}
	body := []byte(`{"error": "rate_limited"}`)

	receipt := dispatch.NormalizeHTTPResponse("slack", resp, body)
	if receipt.Status != "error" {
		t.Fatalf("expected status 'error', got '%s'", receipt.Status)
	}
	if receipt.Code != "RATE_LIMITED" {
		t.Fatalf("expected code 'RATE_LIMITED', got '%s'", receipt.Code)
	}
	if receipt.RetryAfterS != 12.5 {
		t.Fatalf("expected retry_after_s 12.5, got %v", receipt.RetryAfterS)
	}
	if !receipt.Retryable {
		t.Fatalf("expected rate limited request to be marked retryable")
	}
}

func TestErrorNormalizer_NormalizeHTTPResponse_Unauthorized(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusUnauthorized,
		Header:     make(http.Header),
	}
	body := []byte(`{"message": "invalid_auth"}`)

	receipt := dispatch.NormalizeHTTPResponse("slack", resp, body)
	if receipt.Code != "UNAUTHORIZED" {
		t.Fatalf("expected code 'UNAUTHORIZED', got '%s'", receipt.Code)
	}
	if receipt.Retryable {
		t.Fatalf("unauthorized should not be retryable")
	}
}

func TestErrorNormalizer_NormalizeError_ConnectionTimeout(t *testing.T) {
	err := errors.New("dial tcp: i/o timeout")
	receipt := dispatch.NormalizeError("jira", err)

	if receipt.Status != "error" {
		t.Fatalf("expected status 'error', got '%s'", receipt.Status)
	}
	if receipt.Code != "CONNECTION_ERROR" {
		t.Fatalf("expected code 'CONNECTION_ERROR', got '%s'", receipt.Code)
	}
	if !receipt.Retryable {
		t.Fatalf("connection timeouts should be marked retryable")
	}
}
