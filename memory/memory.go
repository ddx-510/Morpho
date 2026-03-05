package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Entry is a single memory item.
type Entry struct {
	Tick      int       `json:"tick"`
	Timestamp time.Time `json:"timestamp"`
	AgentID   string    `json:"agent_id"`
	Category  string    `json:"category"` // "observation", "finding", "action", "tool_result"
	Content   string    `json:"content"`
}

func (e Entry) String() string {
	return fmt.Sprintf("[t%d %s] %s: %s", e.Tick, e.Category, e.AgentID, e.Content)
}

// ShortTerm is per-agent working memory that resets or rolls over per tick.
type ShortTerm struct {
	capacity int
	entries  []Entry
}

// NewShortTerm creates a short-term memory with the given capacity.
func NewShortTerm(capacity int) *ShortTerm {
	return &ShortTerm{capacity: capacity}
}

// Add stores an entry, evicting the oldest if at capacity.
func (m *ShortTerm) Add(e Entry) {
	if len(m.entries) >= m.capacity {
		m.entries = m.entries[1:]
	}
	m.entries = append(m.entries, e)
}

// Recent returns the last n entries.
func (m *ShortTerm) Recent(n int) []Entry {
	if n >= len(m.entries) {
		return m.entries
	}
	return m.entries[len(m.entries)-n:]
}

// All returns all entries.
func (m *ShortTerm) All() []Entry {
	return m.entries
}

// Summary returns a text summary suitable for injecting into LLM context.
func (m *ShortTerm) Summary() string {
	if len(m.entries) == 0 {
		return "(no working memory)"
	}
	var sb strings.Builder
	for _, e := range m.entries {
		sb.WriteString(e.String())
		sb.WriteString("\n")
	}
	return sb.String()
}

// Clear resets working memory.
func (m *ShortTerm) Clear() {
	m.entries = m.entries[:0]
}

// LongTerm is a shared persistent memory store backed by a JSON file.
type LongTerm struct {
	mu      sync.RWMutex
	path    string
	entries []Entry
}

// NewLongTerm creates or loads a long-term memory store.
func NewLongTerm(path string) *LongTerm {
	lt := &LongTerm{path: path}
	lt.load()
	return lt
}

func (m *LongTerm) load() {
	data, err := os.ReadFile(m.path)
	if err != nil {
		return
	}
	json.Unmarshal(data, &m.entries)
}

// Save persists memory to disk.
func (m *LongTerm) Save() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	data, err := json.MarshalIndent(m.entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.path, data, 0644)
}

// Store adds an entry to long-term memory and persists.
func (m *LongTerm) Store(e Entry) {
	m.mu.Lock()
	m.entries = append(m.entries, e)
	m.mu.Unlock()
	m.Save()
}

// Query returns entries matching a category and/or containing a substring.
func (m *LongTerm) Query(category string, contains string) []Entry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var results []Entry
	for _, e := range m.entries {
		if category != "" && e.Category != category {
			continue
		}
		if contains != "" && !strings.Contains(e.Content, contains) {
			continue
		}
		results = append(results, e)
	}
	return results
}

// ByAgent returns all entries from a specific agent.
func (m *LongTerm) ByAgent(agentID string) []Entry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var results []Entry
	for _, e := range m.entries {
		if e.AgentID == agentID {
			results = append(results, e)
		}
	}
	return results
}

// All returns all long-term entries.
func (m *LongTerm) All() []Entry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cp := make([]Entry, len(m.entries))
	copy(cp, m.entries)
	return cp
}

// Summary returns a text summary of all long-term memories.
func (m *LongTerm) Summary() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.entries) == 0 {
		return "(no long-term memories)"
	}
	var sb strings.Builder
	for _, e := range m.entries {
		sb.WriteString(e.String())
		sb.WriteString("\n")
	}
	return sb.String()
}

// Count returns the number of stored memories.
func (m *LongTerm) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries)
}
