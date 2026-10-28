package dispatch

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// EgressReceipt is the standardized machine-readable result returned to calling agents.
type EgressReceipt struct {
	Status      string  `json:"status"` // "success" or "error"
	Channel     string  `json:"channel"`
	StatusCode  int     `json:"status_code,omitempty"`
	Code        string  `json:"code,omitempty"` // e.g. RATE_LIMITED, UNAUTHORIZED, DOWNSTREAM_ERROR
	Message     string  `json:"message"`
	RetryAfterS float64 `json:"retry_after_s,omitempty"`
	Retryable   bool    `json:"retryable"`
	Data        any     `json:"data,omitempty"`
}

func parseRetryAfter(header http.Header) float64 {
	val := header.Get("Retry-After")
	if val == "" {
		return 0
	}
	if parsed, err := strconv.ParseFloat(val, 64); err == nil {
		return parsed
	}
	return 30.0
}

// NormalizeHTTPResponse maps an HTTP response and raw body into an EgressReceipt.
func NormalizeHTTPResponse(channel string, resp *http.Response, body []byte) *EgressReceipt {
	var parsedBody any
	if len(body) > 0 {
		_ = json.Unmarshal(body, &parsedBody)
	}

	statusCode := resp.StatusCode
	retryAfter := parseRetryAfter(resp.Header)

	if statusCode >= 200 && statusCode < 300 {
		return &EgressReceipt{
			Status:     "success",
			Channel:    channel,
			StatusCode: statusCode,
			Code:       "",
			Message:    fmt.Sprintf("Request to channel '%s' succeeded with HTTP %d", channel, statusCode),
			Retryable:  false,
			Data:       parsedBody,
		}
	}

	var code string
	var retryable bool

	switch statusCode {
	case http.StatusBadRequest:
		code = "BAD_REQUEST"
		retryable = false
	case http.StatusUnauthorized:
		code = "UNAUTHORIZED"
		retryable = false
	case http.StatusForbidden:
		code = "FORBIDDEN"
		retryable = false
	case http.StatusNotFound:
		code = "NOT_FOUND"
		retryable = false
	case http.StatusTooManyRequests:
		code = "RATE_LIMITED"
		retryable = true
		if retryAfter == 0 {
			retryAfter = 15.0
		}
	default:
		if statusCode >= 500 {
			code = "DOWNSTREAM_ERROR"
			retryable = true
		} else {
			code = "API_ERROR"
			retryable = false
		}
	}

	errMsg := fmt.Sprintf("Downstream channel '%s' returned HTTP %d (%s)", channel, statusCode, code)
	if m, ok := parsedBody.(map[string]any); ok {
		if msg, exists := m["message"].(string); exists && msg != "" {
			errMsg = msg
		} else if errVal, exists := m["error"].(string); exists && errVal != "" {
			errMsg = errVal
		}
	}

	return &EgressReceipt{
		Status:      "error",
		Channel:     channel,
		StatusCode:  statusCode,
		Code:        code,
		Message:     errMsg,
		RetryAfterS: retryAfter,
		Retryable:   retryable,
		Data:        parsedBody,
	}
}

// NormalizeError converts network exceptions and timeouts into structured receipts.
func NormalizeError(channel string, err error) *EgressReceipt {
	msg := err.Error()
	code := "CONNECTION_ERROR"
	retryable := true

	if strings.Contains(strings.ToLower(msg), "rate limit") {
		code = "RATE_LIMITED"
	}

	return &EgressReceipt{
		Status:      "error",
		Channel:     channel,
		StatusCode:  0,
		Code:        code,
		Message:     msg,
		RetryAfterS: 5.0,
		Retryable:   retryable,
		Data:        nil,
	}
}
