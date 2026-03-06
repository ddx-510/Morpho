package event

import "sync"

// Type identifies what kind of event occurred.
type Type string

const (
	// Chat-level events.
	UserMessage      Type = "user_message"
	AssistantMessage Type = "assistant_message"
	SystemMessage    Type = "system_message"

	// Engine-level events (mapped from engine.ProgressEvent).
	TickStart    Type = "tick_start"
	AgentSpawn   Type = "agent_spawn"
	AgentDiff    Type = "agent_differentiate"
	AgentMove    Type = "agent_move"
	AgentWork    Type = "agent_work_start"
	AgentDone    Type = "agent_work_done"
	AgentDivide  Type = "agent_divide"
	AgentDeath   Type = "agent_death"
	RunComplete  Type = "run_complete"

	// Field state events.
	FieldState   Type = "field_state"

	// Thinking/reasoning events.
	Thinking     Type = "thinking"
	ToolUse      Type = "tool_use"
	ToolResult   Type = "tool_result"
)

// Event is a unified event emitted during chat and morpho runs.
type Event struct {
	Type    Type              `json:"type"`
	Content string            `json:"content,omitempty"`
	Meta    map[string]string `json:"meta,omitempty"`
}

// Hook is a function called when an event occurs.
type Hook func(Event)

// Bus is a publish-subscribe event bus for all Morpho events.
type Bus struct {
	mu    sync.RWMutex
	hooks map[Type][]Hook
	all   []Hook // hooks that receive every event
}

// NewBus creates a new event bus.
func NewBus() *Bus {
	return &Bus{hooks: make(map[Type][]Hook)}
}

// On registers a hook for a specific event type.
func (b *Bus) On(t Type, fn Hook) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.hooks[t] = append(b.hooks[t], fn)
}

// OnAll registers a hook that receives every event.
func (b *Bus) OnAll(fn Hook) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.all = append(b.all, fn)
}

// Emit sends an event to all matching hooks.
func (b *Bus) Emit(ev Event) {
	b.mu.RLock()
	hooks := b.hooks[ev.Type]
	all := b.all
	b.mu.RUnlock()

	for _, fn := range hooks {
		fn(ev)
	}
	for _, fn := range all {
		fn(ev)
	}
}
