package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// OpenAIProvider calls the OpenAI Chat Completions API.
type OpenAIProvider struct {
	APIKey  string
	Model   string
	BaseURL string // defaults to https://api.openai.com/v1
}

func (p *OpenAIProvider) Name() string {
	return fmt.Sprintf("OpenAI(%s)", p.Model)
}

func (p *OpenAIProvider) baseURL() string {
	if p.BaseURL != "" {
		return p.BaseURL
	}
	return "https://api.openai.com/v1"
}

func (p *OpenAIProvider) Generate(req Request) Response {
	messages := []map[string]string{}
	if req.SystemPrompt != "" {
		messages = append(messages, map[string]string{"role": "system", "content": req.SystemPrompt})
	}
	messages = append(messages, map[string]string{"role": "user", "content": req.UserPrompt})

	body := map[string]any{
		"model":    p.Model,
		"messages": messages,
	}

	// Add tools if provided.
	if len(req.Tools) > 0 {
		var tools []map[string]any
		for _, t := range req.Tools {
			props := map[string]any{}
			for name, desc := range t.Parameters {
				props[name] = map[string]any{"type": "string", "description": desc}
			}
			tools = append(tools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        t.Name,
					"description": t.Description,
					"parameters": map[string]any{
						"type":       "object",
						"properties": props,
					},
				},
			})
		}
		body["tools"] = tools
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return Response{Err: fmt.Errorf("marshal: %w", err)}
	}

	httpReq, err := http.NewRequest("POST", p.baseURL()+"/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return Response{Err: fmt.Errorf("request: %w", err)}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)

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
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return Response{Err: fmt.Errorf("unmarshal: %w", err)}
	}

	if len(result.Choices) == 0 {
		return Response{Err: fmt.Errorf("no choices in response")}
	}

	llmResp := Response{Content: result.Choices[0].Message.Content}

	for _, tc := range result.Choices[0].Message.ToolCalls {
		var args map[string]string
		json.Unmarshal([]byte(tc.Function.Arguments), &args)
		llmResp.ToolCalls = append(llmResp.ToolCalls, ToolCall{
			Name: tc.Function.Name,
			Args: args,
		})
	}

	return llmResp
}
