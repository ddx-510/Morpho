package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// ClaudeProvider calls the Anthropic Messages API.
type ClaudeProvider struct {
	APIKey  string
	Model   string // e.g. claude-sonnet-4-20250514
	BaseURL string
}

func (p *ClaudeProvider) Name() string {
	return fmt.Sprintf("Claude(%s)", p.Model)
}

func (p *ClaudeProvider) baseURL() string {
	if p.BaseURL != "" {
		return p.BaseURL
	}
	return "https://api.anthropic.com"
}

func (p *ClaudeProvider) Generate(req Request) Response {
	messages := []map[string]string{
		{"role": "user", "content": req.UserPrompt},
	}

	body := map[string]any{
		"model":      p.Model,
		"max_tokens": 1024,
		"messages":   messages,
	}
	if req.SystemPrompt != "" {
		body["system"] = req.SystemPrompt
	}

	if len(req.Tools) > 0 {
		var tools []map[string]any
		for _, t := range req.Tools {
			props := map[string]any{}
			for name, desc := range t.Parameters {
				props[name] = map[string]any{"type": "string", "description": desc}
			}
			tools = append(tools, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"input_schema": map[string]any{
					"type":       "object",
					"properties": props,
				},
			})
		}
		body["tools"] = tools
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return Response{Err: fmt.Errorf("marshal: %w", err)}
	}

	httpReq, err := http.NewRequest("POST", p.baseURL()+"/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return Response{Err: fmt.Errorf("request: %w", err)}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return Response{Err: fmt.Errorf("http: %w", err)}
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{Err: fmt.Errorf("read: %w", err)}
	}

	if resp.StatusCode != 200 {
		return Response{Err: fmt.Errorf("API %d: %s", resp.StatusCode, string(data))}
	}

	var result struct {
		Content []struct {
			Type  string `json:"type"`
			Text  string `json:"text"`
			ID    string `json:"id"`
			Name  string `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return Response{Err: fmt.Errorf("unmarshal: %w", err)}
	}

	llmResp := Response{}
	for _, block := range result.Content {
		switch block.Type {
		case "text":
			llmResp.Content += block.Text
		case "tool_use":
			var args map[string]string
			json.Unmarshal(block.Input, &args)
			llmResp.ToolCalls = append(llmResp.ToolCalls, ToolCall{
				Name: block.Name,
				Args: args,
			})
		}
	}
	return llmResp
}
