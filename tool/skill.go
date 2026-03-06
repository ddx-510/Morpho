package tool

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ddx-510/Morpho/llm"
)

// Skill is a higher-level reusable capability that composes tools and LLM calls.
type Skill struct {
	SkillName string
	Desc      string
	Schema    json.RawMessage
	Fn        func(args map[string]any) Result
}

func (s *Skill) Name() string                        { return s.SkillName }
func (s *Skill) Description() string                 { return s.Desc }
func (s *Skill) Parameters() json.RawMessage          { return s.Schema }
func (s *Skill) Execute(args map[string]any) Result { return s.Fn(args) }

// WebFetch fetches a URL and returns the text content.
func WebFetch() Tool {
	return &Skill{
		SkillName: "web_fetch",
		Desc:      "Fetch a URL and return its text content (useful for reading docs, APIs, web pages)",
		Schema: llm.ParamSchema(map[string]llm.ParamDef{
			"url": {Description: "URL to fetch", Required: true},
		}),
		Fn: func(args map[string]any) Result {
			url := StringArg(args, "url")
			if url == "" {
				return Result{Err: fmt.Errorf("url is required")}
			}
			client := &http.Client{Timeout: 30 * time.Second}
			resp, err := client.Get(url)
			if err != nil {
				return Result{Err: fmt.Errorf("fetch: %w", err)}
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(io.LimitReader(resp.Body, 100_000))
			if err != nil {
				return Result{Err: fmt.Errorf("read body: %w", err)}
			}

			text := stripHTML(string(body))
			if len(text) > 8000 {
				text = text[:8000] + "\n... (truncated)"
			}
			return Result{Output: text}
		},
	}
}

// Summarize uses the LLM to condense long text into a summary.
func Summarize(provider llm.Provider) Tool {
	return &Skill{
		SkillName: "summarize",
		Desc:      "Summarize long text into key points using LLM",
		Schema: llm.ParamSchema(map[string]llm.ParamDef{
			"text":  {Description: "The text to summarize", Required: true},
			"focus": {Description: "Optional focus area (e.g. 'security issues', 'main arguments')"},
		}),
		Fn: func(args map[string]any) Result {
			text := StringArg(args, "text")
			if text == "" {
				return Result{Err: fmt.Errorf("text is required")}
			}
			if len(text) > 12000 {
				text = text[:12000]
			}
			focus := StringArg(args, "focus")
			prompt := "Summarize the following text into concise key points."
			if focus != "" {
				prompt = fmt.Sprintf("Summarize the following text, focusing on: %s", focus)
			}

			resp := provider.Generate(llm.Request{
				SystemPrompt: prompt,
				UserPrompt:   text,
			})
			if resp.Err != nil {
				return Result{Err: resp.Err}
			}
			return Result{Output: resp.Content}
		},
	}
}

// Extract pulls structured information from text using the LLM.
func Extract(provider llm.Provider) Tool {
	return &Skill{
		SkillName: "extract",
		Desc:      "Extract structured information from text (e.g. names, dates, URLs, patterns)",
		Schema: llm.ParamSchema(map[string]llm.ParamDef{
			"text":   {Description: "The text to extract from", Required: true},
			"schema": {Description: "What to extract (e.g. 'all function names', 'email addresses')", Required: true},
		}),
		Fn: func(args map[string]any) Result {
			text := StringArg(args, "text")
			schema := StringArg(args, "schema")
			if text == "" || schema == "" {
				return Result{Err: fmt.Errorf("text and schema are required")}
			}
			if len(text) > 12000 {
				text = text[:12000]
			}

			resp := provider.Generate(llm.Request{
				SystemPrompt: fmt.Sprintf("Extract the following from the text: %s\nOutput as a clean list, one item per line. No explanations.", schema),
				UserPrompt:   text,
			})
			if resp.Err != nil {
				return Result{Err: resp.Err}
			}
			return Result{Output: resp.Content}
		},
	}
}

// Transform rewrites text according to instructions using the LLM.
func Transform(provider llm.Provider) Tool {
	return &Skill{
		SkillName: "transform",
		Desc:      "Transform text according to instructions (translate, reformat, convert, etc.)",
		Schema: llm.ParamSchema(map[string]llm.ParamDef{
			"text":        {Description: "The text to transform", Required: true},
			"instruction": {Description: "How to transform it (e.g. 'convert to JSON', 'translate to Spanish')", Required: true},
		}),
		Fn: func(args map[string]any) Result {
			text := StringArg(args, "text")
			instruction := StringArg(args, "instruction")
			if text == "" || instruction == "" {
				return Result{Err: fmt.Errorf("text and instruction are required")}
			}
			if len(text) > 12000 {
				text = text[:12000]
			}

			resp := provider.Generate(llm.Request{
				SystemPrompt: fmt.Sprintf("Transform the following text: %s\nOutput only the result.", instruction),
				UserPrompt:   text,
			})
			if resp.Err != nil {
				return Result{Err: resp.Err}
			}
			return Result{Output: resp.Content}
		},
	}
}

// Reason performs multi-step reasoning on a problem using the LLM.
func Reason(provider llm.Provider) Tool {
	return &Skill{
		SkillName: "reason",
		Desc:      "Think through a complex problem step-by-step using chain-of-thought reasoning",
		Schema: llm.ParamSchema(map[string]llm.ParamDef{
			"problem": {Description: "The problem or question to reason about", Required: true},
			"context": {Description: "Optional additional context"},
		}),
		Fn: func(args map[string]any) Result {
			problem := StringArg(args, "problem")
			if problem == "" {
				return Result{Err: fmt.Errorf("problem is required")}
			}
			ctx := StringArg(args, "context")
			userPrompt := problem
			if ctx != "" {
				userPrompt = fmt.Sprintf("Context:\n%s\n\nProblem:\n%s", ctx, problem)
			}

			resp := provider.Generate(llm.Request{
				SystemPrompt: "Think through this problem step by step. Show your reasoning, then give a clear conclusion.",
				UserPrompt:   userPrompt,
			})
			if resp.Err != nil {
				return Result{Err: resp.Err}
			}
			return Result{Output: resp.Content}
		},
	}
}

// RegisterSkills registers all built-in skills into the tool registry.
func RegisterSkills(registry *Registry, provider llm.Provider) {
	registry.Register(WebFetch())
	registry.Register(Summarize(provider))
	registry.Register(Extract(provider))
	registry.Register(Transform(provider))
	registry.Register(Reason(provider))
}

// stripHTML removes HTML tags and returns plain text.
func stripHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
			b.WriteRune(' ')
		case !inTag:
			b.WriteRune(r)
		}
	}
	result := b.String()
	for strings.Contains(result, "  ") {
		result = strings.ReplaceAll(result, "  ", " ")
	}
	return strings.TrimSpace(result)
}
