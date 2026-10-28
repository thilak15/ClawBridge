package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// Server implements a Model Context Protocol (MCP) JSON-RPC 2.0 server.
type Server struct {
	name    string
	version string
	tools   *Tools
	writeMu sync.Mutex
}

// NewServer creates a new MCP Server instance.
func NewServer(name, version string, tools *Tools) *Server {
	if name == "" {
		name = "ClawBridge"
	}
	if version == "" {
		version = "0.1.0"
	}
	return &Server{
		name:    name,
		version: version,
		tools:   tools,
	}
}

type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      any           `json:"id"`
	Result  any           `json:"result,omitempty"`
	Error   *jsonrpcError `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// HandleMessage decodes and executes a single JSON-RPC request and returns the serialized response.
func (s *Server) HandleMessage(ctx context.Context, reqBytes []byte) ([]byte, error) {
	var req jsonrpcRequest
	if err := json.Unmarshal(reqBytes, &req); err != nil {
		return json.Marshal(jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      nil,
			Error: &jsonrpcError{
				Code:    -32700,
				Message: "Parse error",
			},
		})
	}

	switch req.Method {
	case "initialize":
		res := map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{
					"listChanged": false,
				},
			},
			"serverInfo": map[string]any{
				"name":    s.name,
				"version": s.version,
			},
		}
		return json.Marshal(jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  res,
		})

	case "notifications/initialized":
		// Acknowledgement notification, no response required unless ID provided
		if req.ID != nil {
			return json.Marshal(jsonrpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  map[string]any{},
			})
		}
		return nil, nil

	case "tools/list":
		list := s.tools.ListTools()
		return json.Marshal(jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"tools": list,
			},
		})

	case "tools/call":
		var callParams struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &callParams); err != nil {
			return json.Marshal(jsonrpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &jsonrpcError{
					Code:    -32602,
					Message: "Invalid params: failed to parse tool call arguments",
				},
			})
		}

		receipt, err := s.tools.CallTool(ctx, callParams.Name, callParams.Arguments)
		if err != nil {
			return json.Marshal(jsonrpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &jsonrpcError{
					Code:    -32000,
					Message: err.Error(),
				},
			})
		}

		receiptBytes, _ := json.Marshal(receipt)
		content := []map[string]any{
			{
				"type": "text",
				"text": string(receiptBytes),
			},
		}

		return json.Marshal(jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"content": content,
				"isError": receipt.Status == "error",
			},
		})

	default:
		return json.Marshal(jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &jsonrpcError{
				Code:    -32601,
				Message: fmt.Sprintf("Method '%s' not found", req.Method),
			},
		})
	}
}

// ServeStdio handles streaming JSON-RPC requests over an input and output stream.
func (s *Server) ServeStdio(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	// Buffer up to 10MB per line for large tool arguments
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		respBytes, err := s.HandleMessage(ctx, line)
		if err != nil {
			return err
		}

		if len(respBytes) > 0 {
			s.writeMu.Lock()
			_, err = out.Write(append(respBytes, '\n'))
			s.writeMu.Unlock()
			if err != nil {
				return fmt.Errorf("failed to write to output: %w", err)
			}
		}
	}

	return scanner.Err()
}
