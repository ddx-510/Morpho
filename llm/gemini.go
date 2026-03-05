package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// GeminiProvider calls the Google Gemini API.
type GeminiProvider struct {
	APIKey string
	Model  string // e.g. gemini-2.0-flash
}

func (p *GeminiProvider) Name() string {
	return fmt.Sprintf("Gemini(%s)", p.Model)
}

func (p *GeminiProvider) Generate(req Request) Response {
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

	body := map[string]any{
		"contents": contents,
	}

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
				"parameters": map[string]any{
					"type":       "OBJECT",
					"properties": props,
				},
			})
		}
		body["tools"] = []map[string]any{
			{"function_declarations": funcDecls},
		}
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return Response{Err: fmt.Errorf("marshal: %w", err)}
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		p.Model, p.APIKey)

	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return Response{Err: fmt.Errorf("request: %w", err)}
	}
	httpReq.Header.Set("Content-Type", "application/json")

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

	llmResp := Response{}
	for _, part := range result.Candidates[0].Content.Parts {
		if part.Text != "" {
			llmResp.Content += part.Text
		}
		if part.FunctionCall != nil {
			llmResp.ToolCalls = append(llmResp.ToolCalls, ToolCall{
				Name: part.FunctionCall.Name,
				Args: part.FunctionCall.Args,
			})
		}
	}
	return llmResp
}
