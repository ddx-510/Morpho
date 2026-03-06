package tool

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
)

// MCPClient connects to an MCP server via stdio JSON-RPC and exposes its tools.
type MCPClient struct {
	name    string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	mu      sync.Mutex
	nextID  atomic.Int64
	tools   []MCPToolDef
	started bool
}

// MCPToolDef is a tool definition returned by the MCP server.
type MCPToolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// jsonRPCRequest is a JSON-RPC 2.0 request.
type jsonRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// jsonRPCResponse is a JSON-RPC 2.0 response.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCPServerConfig describes how to start an MCP server.
type MCPServerConfig struct {
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
}

// NewMCPClient creates a client for an MCP server but doesn't start it yet.
func NewMCPClient(name string, command string, args []string, env map[string]string) *MCPClient {
	cmd := exec.Command(command, args...)
	for k, v := range env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	return &MCPClient{
		name: name,
		cmd:  cmd,
	}
}

// Start launches the MCP server process, initializes the connection, and discovers tools.
func (c *MCPClient) Start() error {
	var err error
	c.stdin, err = c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("mcp %s: stdin pipe: %w", c.name, err)
	}
	stdoutPipe, err := c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("mcp %s: stdout pipe: %w", c.name, err)
	}
	c.stdout = bufio.NewReader(stdoutPipe)

	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("mcp %s: start: %w", c.name, err)
	}
	c.started = true

	resp, err := c.call("initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]interface{}{
			"name":    "morpho",
			"version": "1.0.0",
		},
	})
	if err != nil {
		c.Stop()
		return fmt.Errorf("mcp %s: initialize: %w", c.name, err)
	}
	_ = resp

	c.notify("notifications/initialized", nil)

	toolsResp, err := c.call("tools/list", map[string]interface{}{})
	if err != nil {
		c.Stop()
		return fmt.Errorf("mcp %s: tools/list: %w", c.name, err)
	}

	var toolsList struct {
		Tools []MCPToolDef `json:"tools"`
	}
	if err := json.Unmarshal(toolsResp, &toolsList); err != nil {
		c.Stop()
		return fmt.Errorf("mcp %s: parse tools: %w", c.name, err)
	}
	c.tools = toolsList.Tools
	return nil
}

// Stop terminates the MCP server process.
func (c *MCPClient) Stop() {
	if c.stdin != nil {
		c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
		c.cmd.Wait()
	}
}

// Tools returns the discovered tool definitions.
func (c *MCPClient) Tools() []MCPToolDef {
	return c.tools
}

// CallTool invokes a tool on the MCP server.
func (c *MCPClient) CallTool(name string, args map[string]interface{}) (string, error) {
	resp, err := c.call("tools/call", map[string]interface{}{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return "", err
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return string(resp), nil
	}

	var texts []string
	for _, c := range result.Content {
		if c.Text != "" {
			texts = append(texts, c.Text)
		}
	}
	if result.IsError && len(texts) > 0 {
		return "", fmt.Errorf("mcp tool error: %s", texts[0])
	}
	if len(texts) > 0 {
		return texts[0], nil
	}
	return string(resp), nil
}

// call sends a JSON-RPC request and waits for the response.
func (c *MCPClient) call(method string, params interface{}) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.nextID.Add(1)
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')

	if _, err := c.stdin.Write(data); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}

	for {
		line, err := c.stdout.ReadBytes('\n')
		if err != nil {
			return nil, fmt.Errorf("read: %w", err)
		}

		var resp jsonRPCResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}
		if resp.ID != id {
			continue
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

// notify sends a JSON-RPC notification (no id, no response).
func (c *MCPClient) notify(method string, params interface{}) {
	msg := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if params != nil {
		msg["params"] = params
	}
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	c.stdin.Write(data)
}

// RegisterMCPTools connects to an MCP server and registers all its tools into the registry.
func RegisterMCPTools(registry *Registry, cfg MCPServerConfig) (*MCPClient, error) {
	client := NewMCPClient(cfg.Name, cfg.Command, cfg.Args, cfg.Env)
	if err := client.Start(); err != nil {
		return nil, err
	}

	for _, td := range client.Tools() {
		registry.Register(&mcpToolWrapper{
			client: client,
			def:    td,
			prefix: cfg.Name,
		})
	}
	return client, nil
}

// mcpToolWrapper wraps an MCP tool as a tool.Tool.
type mcpToolWrapper struct {
	client *MCPClient
	def    MCPToolDef
	prefix string
}

func (t *mcpToolWrapper) Name() string {
	return fmt.Sprintf("mcp_%s_%s", t.prefix, t.def.Name)
}

func (t *mcpToolWrapper) Description() string {
	return fmt.Sprintf("[MCP:%s] %s", t.prefix, t.def.Description)
}

func (t *mcpToolWrapper) Parameters() json.RawMessage {
	data, err := json.Marshal(t.def.InputSchema)
	if err != nil {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	return data
}

func (t *mcpToolWrapper) Execute(args map[string]any) Result {
	mcpArgs := make(map[string]interface{}, len(args))
	for k, v := range args {
		mcpArgs[k] = v
	}
	output, err := t.client.CallTool(t.def.Name, mcpArgs)
	if err != nil {
		return Result{Err: err}
	}
	if len(output) > 8000 {
		output = output[:8000] + "\n... (truncated)"
	}
	return Result{Output: output}
}
