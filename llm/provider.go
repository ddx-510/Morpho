package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// APIFormat describes the request/response shape for an LLM API.
type APIFormat int

const (
	FormatOpenAI    APIFormat = iota // OpenAI, OpenRouter, Groq, Together, local vLLM, etc.
	FormatAnthropic                  // Anthropic Messages API
	FormatGemini                     // Google Gemini generateContent API
)

// HTTPProvider is a single unified provider that handles all API formats.
type HTTPProvider struct {
	Label   string    // display name
	APIKey  string
	Model   string
	BaseURL string    // full base URL (no trailing slash)
	Format  APIFormat
	Headers map[string]string // extra headers
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
	var messages []map[string]any

	if len(req.Messages) > 0 {
		// Multi-turn: use structured messages.
		for _, m := range req.Messages {
			msg := map[string]any{"role": m.Role, "content": m.Content}
			if m.Role == "assistant" && len(m.ToolCalls) > 0 {
				var tcs []map[string]any
				for _, tc := range m.ToolCalls {
					args, _ := json.Marshal(tc.Args)
					tcs = append(tcs, map[string]any{
						"id":   tc.ID,
						"type": "function",
						"function": map[string]any{
							"name":      tc.Name,
							"arguments": string(args),
						},
					})
				}
				msg["tool_calls"] = tcs
				if m.Content == "" {
					delete(msg, "content")
				}
			}
			if m.Role == "tool" {
				msg["tool_call_id"] = m.ToolCallID
			}
			messages = append(messages, msg)
		}
	} else {
		// Simple mode: system + user.
		if req.SystemPrompt != "" {
			messages = append(messages, map[string]any{"role": "system", "content": req.SystemPrompt})
		}
		messages = append(messages, map[string]any{"role": "user", "content": req.UserPrompt})
	}

	body := map[string]any{"model": p.Model, "messages": messages}
	if len(req.Tools) > 0 {
		body["tools"] = openAITools(req.Tools)
	}
	return body
}

func (p *HTTPProvider) buildAnthropic(req Request) map[string]any {
	body := map[string]any{
		"model":      p.Model,
		"max_tokens": 4096,
	}

	if len(req.Messages) > 0 {
		// Multi-turn: convert ChatMessages to Anthropic format.
		var system string
		var messages []map[string]any
		for _, m := range req.Messages {
			if m.Role == "system" {
				system = m.Content
				continue
			}
			if m.Role == "assistant" && len(m.ToolCalls) > 0 {
				// Assistant message with tool calls.
				var content []map[string]any
				if m.Content != "" {
					content = append(content, map[string]any{"type": "text", "text": m.Content})
				}
				for _, tc := range m.ToolCalls {
					content = append(content, map[string]any{
						"type":  "tool_use",
						"id":    tc.ID,
						"name":  tc.Name,
						"input": tc.Args,
					})
				}
				messages = append(messages, map[string]any{"role": "assistant", "content": content})
			} else if m.Role == "tool" {
				// Tool result message.
				messages = append(messages, map[string]any{
					"role": "user",
					"content": []map[string]any{{
						"type":        "tool_result",
						"tool_use_id": m.ToolCallID,
						"content":     m.Content,
					}},
				})
			} else {
				messages = append(messages, map[string]any{"role": m.Role, "content": m.Content})
			}
		}
		body["messages"] = messages
		if system != "" {
			body["system"] = system
		}
	} else {
		body["messages"] = []map[string]string{{"role": "user", "content": req.UserPrompt}}
		if req.SystemPrompt != "" {
			body["system"] = req.SystemPrompt
		}
	}

	if len(req.Tools) > 0 {
		var tools []map[string]any
		for _, t := range req.Tools {
			toolDef := map[string]any{
				"name":        t.Name,
				"description": t.Description,
			}
			if len(t.Parameters) > 0 {
				var schema map[string]any
				json.Unmarshal(t.Parameters, &schema)
				toolDef["input_schema"] = schema
			} else {
				toolDef["input_schema"] = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			tools = append(tools, toolDef)
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
			fd := map[string]any{
				"name":        t.Name,
				"description": t.Description,
			}
			if len(t.Parameters) > 0 {
				var schema map[string]any
				json.Unmarshal(t.Parameters, &schema)
				// Convert JSON Schema types to Gemini types (uppercase).
				fd["parameters"] = convertToGeminiSchema(schema)
			} else {
				fd["parameters"] = map[string]any{"type": "OBJECT", "properties": map[string]any{}}
			}
			funcDecls = append(funcDecls, fd)
		}
		body["tools"] = []map[string]any{{"function_declarations": funcDecls}}
	}
	return body
}

func convertToGeminiSchema(schema map[string]any) map[string]any {
	result := make(map[string]any)
	for k, v := range schema {
		if k == "type" {
			result["type"] = strings.ToUpper(fmt.Sprintf("%v", v))
		} else if k == "properties" {
			if props, ok := v.(map[string]any); ok {
				newProps := make(map[string]any)
				for pk, pv := range props {
					if pm, ok := pv.(map[string]any); ok {
						newProps[pk] = convertToGeminiSchema(pm)
					} else {
						newProps[pk] = pv
					}
				}
				result["properties"] = newProps
			}
		} else {
			result[k] = v
		}
	}
	return result
}

func openAITools(tools []ToolSpec) []map[string]any {
	var out []map[string]any
	for _, t := range tools {
		fn := map[string]any{
			"name":        t.Name,
			"description": t.Description,
		}
		if len(t.Parameters) > 0 {
			var schema any
			json.Unmarshal(t.Parameters, &schema)
			fn["parameters"] = schema
		} else {
			fn["parameters"] = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, map[string]any{
			"type":     "function",
			"function": fn,
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
		// key is in the URL query param
	default:
		r.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
}

// --- response parsing per format ---

func (p *HTTPProvider) parseResponse(data []byte) Response {
	// Try JSON repair before parsing.
	data = repairJSON(data)

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
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return Response{Err: fmt.Errorf("unmarshal: %w", err)}
	}
	if len(result.Choices) == 0 {
		return Response{Err: fmt.Errorf("no choices in response")}
	}

	resp := Response{Content: result.Choices[0].Message.Content}
	resp.Tokens = TokenUsage{
		Input:  result.Usage.PromptTokens,
		Output: result.Usage.CompletionTokens,
		Total:  result.Usage.TotalTokens,
	}
	for _, tc := range result.Choices[0].Message.ToolCalls {
		argsStr := repairJSONString(tc.Function.Arguments)
		var args map[string]any
		json.Unmarshal([]byte(argsStr), &args)
		if args == nil {
			args = make(map[string]any)
		}
		resp.ToolCalls = append(resp.ToolCalls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Args: args})
	}
	return resp
}

func parseAnthropic(data []byte) Response {
	var result struct {
		Content []struct {
			Type  string          `json:"type"`
			ID    string          `json:"id"`
			Text  string          `json:"text"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return Response{Err: fmt.Errorf("unmarshal: %w", err)}
	}

	resp := Response{}
	resp.Tokens = TokenUsage{
		Input:  result.Usage.InputTokens,
		Output: result.Usage.OutputTokens,
		Total:  result.Usage.InputTokens + result.Usage.OutputTokens,
	}
	for _, block := range result.Content {
		switch block.Type {
		case "text":
			resp.Content += block.Text
		case "tool_use":
			var args map[string]any
			json.Unmarshal(block.Input, &args)
			if args == nil {
				args = make(map[string]any)
			}
			resp.ToolCalls = append(resp.ToolCalls, ToolCall{ID: block.ID, Name: block.Name, Args: args})
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
						Name string         `json:"name"`
						Args map[string]any `json:"args"`
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

// --- JSON repair ---

// repairJSON attempts to fix common LLM JSON issues in the raw API response.
func repairJSON(data []byte) []byte {
	// Only repair if it fails to parse.
	var test json.RawMessage
	if json.Unmarshal(data, &test) == nil {
		return data
	}
	return []byte(repairJSONString(string(data)))
}

var (
	reLineComment   = regexp.MustCompile(`(?m)//[^\n]*$`)
	reBlockComment  = regexp.MustCompile(`/\*[\s\S]*?\*/`)
	reTrailingComma = regexp.MustCompile(`,\s*([}\]])`)
)

// repairJSONString fixes common JSON issues from LLM output.
func repairJSONString(s string) string {
	var test json.RawMessage
	if json.Unmarshal([]byte(s), &test) == nil {
		return s
	}

	// Strip comments.
	s = reBlockComment.ReplaceAllString(s, "")
	s = reLineComment.ReplaceAllString(s, "")

	// Trailing commas.
	s = reTrailingComma.ReplaceAllString(s, "$1")

	// Try to balance braces.
	opens := strings.Count(s, "{") - strings.Count(s, "}")
	for opens > 0 {
		s += "}"
		opens--
	}
	opens = strings.Count(s, "[") - strings.Count(s, "]")
	for opens > 0 {
		s += "]"
		opens--
	}

	return s
}
