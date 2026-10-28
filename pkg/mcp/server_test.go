package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"clawbridge/pkg/mcp"
)

func TestMCPServer_HandleInitialize(t *testing.T) {
	tools, serverMock, _ := setupTestTools(t)
	defer serverMock.Close()

	srv := mcp.NewServer("ClawBridge", "0.1.0", tools)

	reqJSON := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"TestClient"}}}`
	respBytes, err := srv.HandleMessage(context.Background(), []byte(reqJSON))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		t.Fatalf("failed to parse json-rpc response: %v", err)
	}

	if resp["id"].(float64) != 1 {
		t.Fatalf("expected id 1, got %v", resp["id"])
	}

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object in initialize response")
	}

	serverInfo, ok := result["serverInfo"].(map[string]any)
	if !ok || serverInfo["name"] != "ClawBridge" {
		t.Fatalf("expected serverInfo name 'ClawBridge', got %v", serverInfo)
	}
}

func TestMCPServer_HandleToolsList(t *testing.T) {
	tools, serverMock, _ := setupTestTools(t)
	defer serverMock.Close()

	srv := mcp.NewServer("ClawBridge", "0.1.0", tools)

	reqJSON := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	respBytes, err := srv.HandleMessage(context.Background(), []byte(reqJSON))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp map[string]any
	_ = json.Unmarshal(respBytes, &resp)

	result := resp["result"].(map[string]any)
	toolsArray := result["tools"].([]any)
	if len(toolsArray) < 4 {
		t.Fatalf("expected at least 4 tools in list, got %d", len(toolsArray))
	}
}

func TestMCPServer_HandleToolsCall_Success(t *testing.T) {
	tools, serverMock, v := setupTestTools(t)
	defer serverMock.Close()

	tokID, _ := v.StoreOAuthToken("slack", "client_slack", map[string]any{"access_token": "token-xyz"}, 3600)

	srv := mcp.NewServer("ClawBridge", "0.1.0", tools)

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "slack_post_message",
			"arguments": map[string]any{
				"channel_id": "C123",
				"text":       "Hello from JSON-RPC call",
				"token_id":   tokID,
				"url":        serverMock.URL,
			},
		},
	}
	reqBytes, _ := json.Marshal(req)

	respBytes, err := srv.HandleMessage(context.Background(), reqBytes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp map[string]any
	_ = json.Unmarshal(respBytes, &resp)

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got %v", resp)
	}

	contentList, ok := result["content"].([]any)
	if !ok || len(contentList) == 0 {
		t.Fatalf("expected content blocks in tools/call result")
	}

	block := contentList[0].(map[string]any)
	if block["type"] != "text" {
		t.Fatalf("expected text block type, got %v", block["type"])
	}
	textStr := block["text"].(string)
	if !strings.Contains(textStr, "success") {
		t.Fatalf("expected receipt text with success, got: %s", textStr)
	}
}

func TestMCPServer_HandleUnknownMethod(t *testing.T) {
	tools, serverMock, _ := setupTestTools(t)
	defer serverMock.Close()

	srv := mcp.NewServer("ClawBridge", "0.1.0", tools)

	reqJSON := `{"jsonrpc":"2.0","id":4,"method":"unknown/endpoint"}`
	respBytes, err := srv.HandleMessage(context.Background(), []byte(reqJSON))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp map[string]any
	_ = json.Unmarshal(respBytes, &resp)

	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object in response")
	}
	code := int(errObj["code"].(float64))
	if code != -32601 { // Method not found
		t.Fatalf("expected JSON-RPC code -32601, got %d", code)
	}
}

func TestMCPServer_ServeStdio(t *testing.T) {
	tools, serverMock, _ := setupTestTools(t)
	defer serverMock.Close()

	srv := mcp.NewServer("ClawBridge", "0.1.0", tools)

	input := `{"jsonrpc":"2.0","id":10,"method":"tools/list","params":{}}` + "\n"
	inBuffer := bytes.NewBufferString(input)
	outBuffer := &bytes.Buffer{}

	err := srv.ServeStdio(context.Background(), inBuffer, outBuffer)
	if err != nil {
		t.Fatalf("unexpected serve error: %v", err)
	}

	output := outBuffer.String()
	if !strings.Contains(output, `"id":10`) || !strings.Contains(output, "slack_post_message") {
		t.Fatalf("unexpected stdio output: %s", output)
	}
}
