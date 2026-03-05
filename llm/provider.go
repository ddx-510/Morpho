package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// APIFormat describes the request/response shape for an LLM API.
type APIFormat int

const (
	FormatOpenAI    APIFormat = iota // OpenAI, OpenRouter, Groq, Together, local vLLM, etc.
	FormatAnthropic                  // Anthropic Messages API
	FormatGemini                     // Google Gemini generateContent API
)

// HTTPProvider is a single unified provider that handles all API formats.
// Adding a new model only requires choosing a format and setting the right URL.
type HTTPProvider struct {
	Label   string    // display name, e.g. "OpenAI" or "OpenRouter"
	APIKey  string
	Model   string
	BaseURL string    // full base URL (no trailing slash)
	Format  APIFormat
	Headers map[string]string // extra headers (e.g. anthropic-version)
}

func (p *HTTPProvider) Name() string {
	return fmt.Sprintf("%s(%s)", p.Label, p.Model)
}

func (p *HTTPProvider) Generate(req Request) Response {
	body, endpoint := p.buildRequest(req)

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return Response{Err: fmt.Errorf("marshal: %w", err)}
	}

	httpReq, err := http.NewRequest("POST", endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return Response{Err: fmt.Errorf("request: %w", err)}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	p.setAuth(httpReq)

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

	return p.parseResponse(data)
}

// --- request building per format ---

func (p *HTTPProvider) buildRequest(req Request) (body map[string]any, endpoint string) {
	switch p.Format {
	case FormatAnthropic:
		return p.buildAnthropic(req), p.BaseURL + "/v1/messages"
	case FormatGemini:
		return p.buildGemini(req), fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", p.BaseURL, p.Model, p.APIKey)
	default:
		return p.buildOpenAI(req), p.BaseURL + "/chat/completions"
	}
}

func (p *HTTPProvider) buildOpenAI(req Request) map[string]any {
	messages := []map[string]string{}
	if req.SystemPrompt != "" {
		messages = append(messages, map[string]string{"role": "system", "content": req.SystemPrompt})
	}
	messages = append(messages, map[string]string{"role": "user", "content": req.UserPrompt})

	body := map[string]any{"model": p.Model, "messages": messages}
	if len(req.Tools) > 0 {
		body["tools"] = openAITools(req.Tools)
	}
	return body
}

func (p *HTTPProvider) buildAnthropic(req Request) map[string]any {
	body := map[string]any{
		"model":      p.Model,
		"max_tokens": 1024,
		"messages":   []map[string]string{{"role": "user", "content": req.UserPrompt}},
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
	return body
}

func (p *HTTPProvider) buildGemini(req Request) map[string]any {
	contents := []map[string]any{}
	if req.SystemPrompt != "" {
		contents = append(contents, map[string]any{
			"role":  "user",
			"parts": []map[string]string{{"text": req.SystemPrompt}},
		})
		contents = append(contents, map[string]any{
			"role":  "model",
			"parts": []map[string]string{{"text": "Understood."}},
		})
	}
	contents = append(contents, map[string]any{
		"role":  "user",
		"parts": []map[string]string{{"text": req.UserPrompt}},
	})

	body := map[string]any{"contents": contents}
	if len(req.Tools) > 0 {
		var funcDecls []map[string]any
		for _, t := range req.Tools {
			props := map[string]any{}
			for name, desc := range t.Parameters {
				props[name] = map[string]any{"type": "STRING", "description": desc}
			}
			funcDecls = append(funcDecls, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  map[string]any{"type": "OBJECT", "properties": props},
			})
		}
		body["tools"] = []map[string]any{{"function_declarations": funcDecls}}
	}
	return body
}

func openAITools(tools []ToolSpec) []map[string]any {
	var out []map[string]any
	for _, t := range tools {
		props := map[string]any{}
		for name, desc := range t.Parameters {
			props[name] = map[string]any{"type": "string", "description": desc}
		}
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  map[string]any{"type": "object", "properties": props},
			},
		})
	}
	return out
}

// --- auth ---

func (p *HTTPProvider) setAuth(r *http.Request) {
	for k, v := range p.Headers {
		r.Header.Set(k, v)
	}
	switch p.Format {
	case FormatAnthropic:
		r.Header.Set("x-api-key", p.APIKey)
		if r.Header.Get("anthropic-version") == "" {
			r.Header.Set("anthropic-version", "2023-06-01")
		}
	case FormatGemini:
		// key is in the URL query param, set in buildRequest
	default:
		r.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
}

// --- response parsing per format ---

func (p *HTTPProvider) parseResponse(data []byte) Response {
	switch p.Format {
	case FormatAnthropic:
		return parseAnthropic(data)
	case FormatGemini:
		return parseGemini(data)
	default:
		return parseOpenAI(data)
	}
}

func parseOpenAI(data []byte) Response {
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

	resp := Response{Content: result.Choices[0].Message.Content}
	for _, tc := range result.Choices[0].Message.ToolCalls {
		var args map[string]string
		json.Unmarshal([]byte(tc.Function.Arguments), &args)
		resp.ToolCalls = append(resp.ToolCalls, ToolCall{Name: tc.Function.Name, Args: args})
	}
	return resp
}

func parseAnthropic(data []byte) Response {
	var result struct {
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return Response{Err: fmt.Errorf("unmarshal: %w", err)}
	}

	resp := Response{}
	for _, block := range result.Content {
		switch block.Type {
		case "text":
			resp.Content += block.Text
		case "tool_use":
			var args map[string]string
			json.Unmarshal(block.Input, &args)
			resp.ToolCalls = append(resp.ToolCalls, ToolCall{Name: block.Name, Args: args})
		}
	}
	return resp
}

func parseGemini(data []byte) Response {
	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text         string `json:"text"`
					FunctionCall *struct {
						Name string            `json:"name"`
						Args map[string]string `json:"args"`
					} `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return Response{Err: fmt.Errorf("unmarshal: %w", err)}
	}
	if len(result.Candidates) == 0 {
		return Response{Err: fmt.Errorf("no candidates in response")}
	}

	resp := Response{}
	for _, part := range result.Candidates[0].Content.Parts {
		if part.Text != "" {
			resp.Content += part.Text
		}
		if part.FunctionCall != nil {
			resp.ToolCalls = append(resp.ToolCalls, ToolCall{Name: part.FunctionCall.Name, Args: part.FunctionCall.Args})
		}
	}
	return resp
}
