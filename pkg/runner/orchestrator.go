package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"clawbridge/pkg/agents"
	"clawbridge/pkg/auth"
	"clawbridge/pkg/inbound"
	"clawbridge/pkg/llm"
	"clawbridge/pkg/mcp"
)

// ANSI Color Codes
const (
	ColorReset   = "\033[0m"
	ColorGreen   = "\033[32m"
	ColorCyan    = "\033[36m"
	ColorMagenta = "\033[35m"
	ColorYellow  = "\033[33m"
	ColorBlue    = "\033[34m"
	ColorRed     = "\033[31m"
)

// RunningAgent represents a live agent running in an independent goroutine.
type RunningAgent struct {
	Config    agents.AgentConfig
	Token     string
	Memory    *agents.SlidingMemory
	ColorCode string
	TaskChan  chan string
	Cancel    context.CancelFunc
}

// Orchestrator coordinates concurrent multi-agent goroutines, tool execution, and logs.
type Orchestrator struct {
	mu          sync.RWMutex
	agents      map[string]*RunningAgent
	agentDir    string
	registry    *auth.AgentRegistry
	llmClient   *llm.Client
	tools       *mcp.Tools
	out         io.Writer
	broadcaster *inbound.EventBroadcaster
}

// NewOrchestrator creates a new multi-agent orchestrator.
func NewOrchestrator(
	agentDir string,
	registry *auth.AgentRegistry,
	llmClient *llm.Client,
	tools *mcp.Tools,
	broadcaster *inbound.EventBroadcaster,
	out io.Writer,
) *Orchestrator {
	if out == nil {
		out = os.Stdout
	}
	return &Orchestrator{
		agents:      make(map[string]*RunningAgent),
		agentDir:    agentDir,
		registry:    registry,
		llmClient:   llmClient,
		tools:       tools,
		broadcaster: broadcaster,
		out:         out,
	}
}

// Log outputs a formatted, color-coded, timestamped log line.
func (o *Orchestrator) Log(colorCode, name, message string) {
	ts := time.Now().UTC().Format("15:04:05")
	if colorCode != "" {
		fmt.Fprintf(o.out, "%s[%s] [%s]%s %s\n", colorCode, ts, name, ColorReset, message)
	} else {
		fmt.Fprintf(o.out, "[%s] [%s] %s\n", ts, name, message)
	}
}

func pickColor(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "nemo"):
		return ColorGreen
	case strings.Contains(lower, "open"):
		return ColorCyan
	case strings.Contains(lower, "muse"):
		return ColorMagenta
	default:
		return ColorYellow
	}
}

// StartAgent initializes and launches an agent goroutine.
func (o *Orchestrator) StartAgent(ctx context.Context, name string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if _, exists := o.agents[name]; exists {
		return fmt.Errorf("agent '%s' is already running", name)
	}

	// 1. Look up agent record in SQLite or load from directory
	var cfg agents.AgentConfig
	if o.agentDir != "" {
		cfgs, err := agents.LoadAgentsFromDir(o.agentDir, o.registry)
		if err == nil {
			for _, c := range cfgs {
				if strings.EqualFold(c.Name, name) {
					cfg = *c
					break
				}
			}
		}
	}

	if cfg.Name == "" {
		cfg = agents.AgentConfig{
			Name:         name,
			SystemPrompt: fmt.Sprintf("You are %s, an autonomous assistant equipped with ClawBridge tools.", name),
			Model:        "gemini-1.5-flash",
			Permissions:  []string{"*"},
		}
	}

	// Ensure registered in DB
	var token string
	if o.registry != nil {
		rec, err := o.registry.GetAgentByName(name)
		if err != nil || rec == nil {
			_, tok, err := o.registry.RegisterAgent(name, cfg.Permissions)
			if err == nil {
				token = tok
			}
		} else {
			cfg.Permissions = rec.AllowedRules
		}
	}

	agentCtx, cancel := context.WithCancel(ctx)
	var memStore agents.MemoryPersistence
	if o.registry != nil && o.registry.DatabaseManager() != nil {
		memStore = o.registry.DatabaseManager()
	}
	running := &RunningAgent{
		Config:    cfg,
		Token:     token,
		Memory:    agents.NewTieredMemory(name, agents.DefaultMaxMemoryTurns, memStore),
		ColorCode: pickColor(name),
		TaskChan:  make(chan string, 100),
		Cancel:    cancel,
	}

	o.agents[name] = running
	o.Log(running.ColorCode, name, fmt.Sprintf("Agent started (Permissions: %v)", cfg.Permissions))

	go o.agentWorker(agentCtx, running)

	return nil
}

// StartAll loads all agents and starts them concurrently.
func (o *Orchestrator) StartAll(ctx context.Context) ([]string, error) {
	var names []string

	// From directory
	if o.agentDir != "" {
		cfgs, err := agents.LoadAgentsFromDir(o.agentDir, o.registry)
		if err == nil {
			for _, c := range cfgs {
				names = append(names, c.Name)
			}
		}
	}

	// From registry
	if o.registry != nil {
		list, err := o.registry.ListAgents()
		if err == nil {
			for _, rec := range list {
				found := false
				for _, n := range names {
					if strings.EqualFold(n, rec.Name) {
						found = true
						break
					}
				}
				if !found {
					names = append(names, rec.Name)
				}
			}
		}
	}

	// Default fallbacks if empty
	if len(names) == 0 {
		names = []string{"nemoclaw", "openclaw"}
	}

	var started []string
	for _, n := range names {
		if err := o.StartAgent(ctx, n); err == nil {
			started = append(started, n)
		}
	}

	return started, nil
}

// StopAgent halts a running agent goroutine.
func (o *Orchestrator) StopAgent(name string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	running, exists := o.agents[name]
	if !exists {
		return fmt.Errorf("agent '%s' is not running", name)
	}

	running.Cancel()
	delete(o.agents, name)
	o.Log(running.ColorCode, name, "Agent stopped.")
	return nil
}

// StopAll halts all running agent goroutines.
func (o *Orchestrator) StopAll() {
	o.mu.Lock()
	defer o.mu.Unlock()

	for name, running := range o.agents {
		running.Cancel()
		o.Log(running.ColorCode, name, "Agent stopped.")
		delete(o.agents, name)
	}
}

// DispatchTask sends a specific instruction to a named agent.
func (o *Orchestrator) DispatchTask(name, instruction string) error {
	o.mu.RLock()
	running, exists := o.agents[name]
	o.mu.RUnlock()

	if !exists {
		return fmt.Errorf("agent '%s' is not running", name)
	}

	select {
	case running.TaskChan <- instruction:
		return nil
	default:
		return fmt.Errorf("agent '%s' task queue is full", name)
	}
}

// BroadcastTask sends an instruction to all currently running agents.
func (o *Orchestrator) BroadcastTask(instruction string) error {
	o.mu.RLock()
	defer o.mu.RUnlock()

	if len(o.agents) == 0 {
		return fmt.Errorf("no agents are currently running")
	}

	for _, running := range o.agents {
		select {
		case running.TaskChan <- instruction:
		default:
		}
	}
	return nil
}

// ListRunning returns names of all active agents.
func (o *Orchestrator) ListRunning() []string {
	o.mu.RLock()
	defer o.mu.RUnlock()

	var list []string
	for name := range o.agents {
		list = append(list, name)
	}
	return list
}

// IsRunning checks if a specific agent is active.
func (o *Orchestrator) IsRunning(name string) bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	_, exists := o.agents[name]
	return exists
}

// GetAgentMemory returns the memory of an agent.
func (o *Orchestrator) GetAgentMemory(name string) *agents.SlidingMemory {
	o.mu.RLock()
	defer o.mu.RUnlock()

	if running, exists := o.agents[name]; exists {
		return running.Memory
	}
	return nil
}

// agentWorker runs the continuous task processing loop for an agent.
func (o *Orchestrator) agentWorker(ctx context.Context, agt *RunningAgent) {
	for {
		select {
		case <-ctx.Done():
			return
		case task, ok := <-agt.TaskChan:
			if !ok {
				return
			}
			o.executeTask(ctx, agt, task)
		}
	}
}

// executeTask runs a task through the LLM client and tools.
func (o *Orchestrator) executeTask(ctx context.Context, agt *RunningAgent, task string) {
	o.Log(agt.ColorCode, agt.Config.Name, fmt.Sprintf("Processing task: %s", task))

	// If no LLM client configured (e.g. offline testing), record in memory and log mock response
	if o.llmClient == nil {
		agt.Memory.AddMessage(llm.Message{Role: "user", Content: task})
		reply := fmt.Sprintf("Completed task '%s'", task)
		agt.Memory.AddMessage(llm.Message{Role: "assistant", Content: reply})
		o.Log(agt.ColorCode, agt.Config.Name, fmt.Sprintf("Result: %s", reply))
		return
	}

	// 1. Prepare tools filtered by agent permissions
	var availableTools []mcp.ToolDefinition
	if o.tools != nil {
		for _, t := range o.tools.ListTools() {
			if isToolAuthorized(agt.Config.Permissions, t.Name) {
				availableTools = append(availableTools, t)
			}
		}
	}

	// 2. Add user prompt to memory
	userMsg := llm.Message{Role: "user", Content: task}
	agt.Memory.AddMessage(userMsg)

	req := llm.ChatRequest{
		Model:        agt.Config.Model,
		SystemPrompt: agt.Config.SystemPrompt,
		Messages:     agt.Memory.GetContextMessages(),
		Tools:        availableTools,
	}

	// Multi-turn tool execution loop
	for turn := 0; turn < 5; turn++ {
		resp, err := o.llmClient.Chat(ctx, req)
		if err != nil {
			o.Log(ColorRed, agt.Config.Name, fmt.Sprintf("LLM error: %v", err))
			return
		}

		if len(resp.ToolCalls) == 0 {
			agt.Memory.AddMessage(llm.Message{Role: "assistant", Content: resp.Content})
			if resp.Content != "" {
				o.Log(agt.ColorCode, agt.Config.Name, fmt.Sprintf("Response: %s", resp.Content))
			}
			return
		}

		// Assistant message with tool calls
		assistantMsg := llm.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		agt.Memory.AddMessage(assistantMsg)
		req.Messages = append(req.Messages, assistantMsg)

		// Execute tool calls
		for _, call := range resp.ToolCalls {
			if !isToolAuthorized(agt.Config.Permissions, call.Name) {
				errMsg := fmt.Sprintf("Permission denied: agent '%s' cannot invoke tool '%s'", agt.Config.Name, call.Name)
				o.Log(ColorRed, agt.Config.Name, errMsg)
				toolMsg := llm.Message{
					Role:       "tool",
					ToolCallID: call.ID,
					Content:    fmt.Sprintf(`{"error":"%s"}`, errMsg),
				}
				agt.Memory.AddMessage(toolMsg)
				req.Messages = append(req.Messages, toolMsg)
				continue
			}

			o.Log(agt.ColorCode, agt.Config.Name, fmt.Sprintf("Calling tool '%s' with args %v", call.Name, call.Arguments))
			res, err := o.tools.CallTool(ctx, call.Name, call.Arguments)
			var resStr string
			if err != nil {
				o.Log(ColorRed, agt.Config.Name, fmt.Sprintf("Tool error: %v", err))
				resStr = fmt.Sprintf(`{"status":"error","message":"%s"}`, err.Error())
			} else {
				b, _ := json.Marshal(res)
				resStr = string(b)
				o.Log(agt.ColorCode, agt.Config.Name, fmt.Sprintf("Tool completed: %s", resStr))
			}

			toolMsg := llm.Message{
				Role:       "tool",
				ToolCallID: call.ID,
				Content:    resStr,
			}
			agt.Memory.AddMessage(toolMsg)
			req.Messages = append(req.Messages, toolMsg)
		}
	}
}

func isToolAuthorized(permissions []string, toolName string) bool {
	channel, action := toolNameToChannelAction(toolName)
	return auth.IsAuthorized(permissions, channel, action)
}

func toolNameToChannelAction(toolName string) (string, string) {
	switch toolName {
	case "slack_post_message":
		return "slack", "post_message"
	case "jira_create_issue":
		return "jira", "create_issue"
	case "discord_send_message":
		return "discord", "send_message"
	case "twitter_post_tweet":
		return "twitter", "post_tweet"
	case "github_create_issue":
		return "github", "create_issue"
	default:
		parts := strings.SplitN(toolName, "_", 2)
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
		return toolName, "*"
	}
}
