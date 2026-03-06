package chat

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ddx-510/Morpho/llm"
)

// Strategy is the execution tier for a message.
type Strategy string

const (
	ChatTier   Strategy = "chat"
	AssistTier Strategy = "assist"
	SwarmTier  Strategy = "swarm"
)

// Plan is what the router produces for each incoming message.
type Plan struct {
	Strategy Strategy `json:"strategy"`
	Reason   string   `json:"reason"`
	Task     string   `json:"task"`
}

// Router routes messages to the appropriate execution tier.
type Router struct {
	provider llm.Provider
}

func newRouter(provider llm.Provider) *Router {
	return &Router{provider: provider}
}

// RouterMessage is a minimal chat message for classification context.
type RouterMessage struct {
	Role    string
	Content string
}

// Classify determines the execution strategy for a message.
func (s *Router) Classify(msg string, history []RouterMessage) Plan {
	resp := s.provider.Generate(llm.Request{
		SystemPrompt: classifyPrompt(history),
		UserPrompt:   msg,
	})

	if resp.Err != nil {
		return Plan{Strategy: ChatTier, Reason: "classification error: " + resp.Err.Error()}
	}

	return parseClassification(resp.Content)
}

func tierOrder(s Strategy) int {
	switch s {
	case ChatTier:
		return 0
	case AssistTier:
		return 1
	case SwarmTier:
		return 2
	}
	return 0
}

func classifyPrompt(history []RouterMessage) string {
	var histCtx string
	if len(history) > 0 {
		start := 0
		if len(history) > 6 {
			start = len(history) - 6
		}
		var sb strings.Builder
		for _, m := range history[start:] {
			content := m.Content
			if len(content) > 200 {
				content = content[:200] + "..."
			}
			fmt.Fprintf(&sb, "%s: %s\n", m.Role, content)
		}
		histCtx = "\n\nRecent conversation:\n" + sb.String()
	}

	return fmt.Sprintf(`You are a task router. Classify the user's message into exactly one execution strategy.

STRATEGIES:
- "chat": Conversational messages. Greetings, thanks, explanations, opinions, simple questions that don't need file access or analysis. Examples: "hi", "what is a goroutine?", "thanks!", "explain the difference between X and Y"
- "assist": Focused tasks that need tools (file reading, search, edits) but target a specific file, function, or narrow question. A single agent with tools can handle this. Examples: "what does the Run method do?", "fix the bug in router.go", "add error handling to this function", "how is config loaded?"
- "swarm": Broad analysis requiring multiple specialist perspectives across many files or regions. Needs the full multi-agent system. Examples: "review this codebase for security issues", "analyze the entire project for performance problems", "do a comprehensive code review", "find all bugs in this project"

Respond with ONLY a JSON object, no other text:
{"strategy": "chat|assist|swarm", "reason": "brief reason", "task": "normalized task description or empty for chat"}%s`, histCtx)
}

func parseClassification(raw string) Plan {
	raw = strings.TrimSpace(raw)

	if idx := strings.Index(raw, "{"); idx >= 0 {
		if end := strings.LastIndex(raw, "}"); end > idx {
			raw = raw[idx : end+1]
		}
	}

	var plan Plan
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		lower := strings.ToLower(raw)
		if strings.Contains(lower, "swarm") {
			return Plan{Strategy: SwarmTier, Reason: "inferred from response"}
		}
		if strings.Contains(lower, "assist") {
			return Plan{Strategy: AssistTier, Reason: "inferred from response"}
		}
		return Plan{Strategy: ChatTier, Reason: "parse fallback"}
	}

	switch plan.Strategy {
	case ChatTier, AssistTier, SwarmTier:
		// valid
	default:
		plan.Strategy = ChatTier
		plan.Reason = "unknown strategy, defaulting to chat"
	}

	return plan
}
