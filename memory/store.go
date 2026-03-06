package memory

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ── Facts (MEMORY.md pattern) ──────────────────────────────────────
// Global persistent knowledge extracted from conversations and swarm runs.
// Survives across all sessions. LLM-consolidated when too large.

// Fact is a single piece of learned knowledge.
type Fact struct {
	ID        string    `json:"id"`
	Topic     string    `json:"topic"`       // "project:X", "user:pref", "code:pattern", "finding:swarm"
	Content   string    `json:"content"`
	Source    string    `json:"source"`       // "chat", "swarm", "user", "assist"
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	AccessCnt int       `json:"access_count"`
}

// FactStore manages persistent facts with size-gated compression.
type FactStore struct {
	mu       sync.RWMutex
	path     string
	facts    []Fact
	maxBytes int // trigger compression above this size (0 = unlimited)
}

// NewFactStore creates or loads a fact store.
func NewFactStore(path string) *FactStore {
	fs := &FactStore{path: path, maxBytes: 32768} // 32KB default
	fs.load()
	return fs
}

func (fs *FactStore) load() {
	data, err := os.ReadFile(fs.path)
	if err != nil {
		return
	}
	json.Unmarshal(data, &fs.facts)
}

func (fs *FactStore) save() error {
	if fs.path == "" {
		return nil
	}
	dir := filepath.Dir(fs.path)
	if dir != "." && dir != "" {
		os.MkdirAll(dir, 0755)
	}
	data, err := json.MarshalIndent(fs.facts, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(fs.path, data, 0644)
}

// Add stores a new fact, deduplicating by content similarity.
func (fs *FactStore) Add(topic, content, source string) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	for i, f := range fs.facts {
		if f.Topic == topic && f.Content == content {
			fs.facts[i].UpdatedAt = time.Now()
			fs.facts[i].AccessCnt++
			fs.save()
			return
		}
	}

	fs.facts = append(fs.facts, Fact{
		ID:        randomID(6),
		Topic:     topic,
		Content:   content,
		Source:    source,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	fs.save()
}

// NeedsCompression returns true if facts exceed max size.
func (fs *FactStore) NeedsCompression() bool {
	if fs.maxBytes == 0 {
		return false
	}
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	data, _ := json.Marshal(fs.facts)
	return len(data) > fs.maxBytes
}

// ReplaceAll atomically replaces all facts (used after LLM compression).
func (fs *FactStore) ReplaceAll(facts []Fact) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.facts = facts
	fs.save()
}

// Search returns facts matching a query (substring on topic + content).
func (fs *FactStore) Search(query string) []Fact {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	q := strings.ToLower(query)
	var results []Fact
	for _, f := range fs.facts {
		if strings.Contains(strings.ToLower(f.Topic), q) ||
			strings.Contains(strings.ToLower(f.Content), q) {
			results = append(results, f)
		}
	}
	return results
}

// Relevant returns the top-n facts most relevant to a query using TF-IDF scoring.
func (fs *FactStore) Relevant(query string, n int) []Fact {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	if len(fs.facts) == 0 || query == "" {
		return nil
	}

	type scored struct {
		fact  Fact
		score float64
	}
	var candidates []scored
	for _, f := range fs.facts {
		doc := f.Topic + " " + f.Content
		s := tfidfScore(query, doc)
		if s > 0.05 { // minimum relevance threshold
			candidates = append(candidates, scored{f, s})
		}
	}

	// Sort by score descending.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	if n > len(candidates) {
		n = len(candidates)
	}
	results := make([]Fact, n)
	for i := 0; i < n; i++ {
		results[i] = candidates[i].fact
	}
	return results
}

// ByTopic returns all facts for a given topic prefix.
func (fs *FactStore) ByTopic(prefix string) []Fact {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	var results []Fact
	for _, f := range fs.facts {
		if strings.HasPrefix(f.Topic, prefix) {
			results = append(results, f)
		}
	}
	return results
}

// All returns all facts.
func (fs *FactStore) All() []Fact {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	cp := make([]Fact, len(fs.facts))
	copy(cp, fs.facts)
	return cp
}

// Remove deletes a fact by ID.
func (fs *FactStore) Remove(id string) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	for i, f := range fs.facts {
		if f.ID == id {
			fs.facts = append(fs.facts[:i], fs.facts[i+1:]...)
			fs.save()
			return
		}
	}
}

// Count returns the number of stored facts.
func (fs *FactStore) Count() int {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return len(fs.facts)
}

// Dump returns all facts as text for LLM context or compression input.
func (fs *FactStore) Dump() string {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	if len(fs.facts) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, f := range fs.facts {
		fmt.Fprintf(&sb, "- [%s] %s (from %s, %s)\n", f.Topic, f.Content, f.Source, f.UpdatedAt.Format("Jan 02"))
	}
	return sb.String()
}

// ── Sessions (JSONL pattern) ───────────────────────────────────────
// Each session is a JSONL file: metadata line + message lines.
// Supports consolidation pointer to track what's been extracted to memory.

// SessionMessage is a message within a session.
type SessionMessage struct {
	Type      string    `json:"_type,omitempty"`         // "metadata" for meta line, empty for messages
	Role      string    `json:"role,omitempty"`
	Content   string    `json:"content,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	Strategy  string    `json:"strategy,omitempty"`      // "chat", "assist", "swarm"
	ToolCalls []string  `json:"tool_calls,omitempty"`    // tool names used in this turn
	Steps     []Step    `json:"steps,omitempty"`         // intermediate steps (tool calls, swarm events)
}

// Step is an intermediate event within a message turn.
type Step struct {
	Kind    string `json:"kind"`              // "tool_use", "tool_result", "thinking", "agent_spawn", "agent_diff", "agent_done", "agent_death", "tick", "complete"
	Agent   string `json:"agent,omitempty"`
	Role    string `json:"role,omitempty"`
	Point   string `json:"point,omitempty"`
	Tool    string `json:"tool,omitempty"`
	Args    string `json:"args,omitempty"`
	Content string `json:"content,omitempty"`
}

// SessionMeta is the first line in a JSONL session file.
type SessionMeta struct {
	Type             string    `json:"_type"`              // always "metadata"
	ID               string    `json:"id"`
	Title            string    `json:"title"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	LastConsolidated int       `json:"last_consolidated"` // index of last message sent to fact extraction
	MessageCount     int       `json:"message_count"`
}

// Session is a loaded session with metadata and messages.
type Session struct {
	Meta     SessionMeta
	Messages []SessionMessage
}

// ID returns the session ID.
func (s *Session) ID() string { return s.Meta.ID }

// SessionStore manages JSONL session files in a directory.
type SessionStore struct {
	mu  sync.Mutex
	dir string
}

// NewSessionStore creates a session store.
func NewSessionStore(dir string) *SessionStore {
	os.MkdirAll(dir, 0755)
	return &SessionStore{dir: dir}
}

// Create starts a new session.
func (ss *SessionStore) Create() *Session {
	now := time.Now()
	meta := SessionMeta{
		Type:      "metadata",
		ID:        randomID(8),
		Title:     "New conversation",
		CreatedAt: now,
		UpdatedAt: now,
	}
	s := &Session{Meta: meta}

	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.writeMeta(meta)
	return s
}

// Load reads a session from disk.
func (ss *SessionStore) Load(id string) (*Session, error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.loadLocked(id)
}

func (ss *SessionStore) loadLocked(id string) (*Session, error) {
	f, err := os.Open(ss.sessionPath(id))
	if err != nil {
		return nil, fmt.Errorf("session %s not found", id)
	}
	defer f.Close()

	s := &Session{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024) // 1MB line buffer

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Check if metadata line.
		var peek struct {
			Type string `json:"_type"`
		}
		json.Unmarshal(line, &peek)

		if peek.Type == "metadata" {
			json.Unmarshal(line, &s.Meta)
		} else {
			var msg SessionMessage
			if err := json.Unmarshal(line, &msg); err == nil {
				s.Messages = append(s.Messages, msg)
			}
		}
	}
	return s, nil
}

// Append adds a message to a session (JSONL append).
func (ss *SessionStore) Append(id string, msg SessionMessage) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	msg.Timestamp = time.Now()

	f, err := os.OpenFile(ss.sessionPath(id), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(f, "%s\n", data)
	if err != nil {
		return err
	}

	// Update metadata (title on first user message, message count).
	s, err := ss.loadLocked(id)
	if err != nil {
		return nil // non-fatal
	}
	s.Meta.UpdatedAt = time.Now()
	s.Meta.MessageCount = len(s.Messages)
	if s.Meta.Title == "" && msg.Role == "user" && msg.Content != "" {
		title := msg.Content
		if len(title) > 60 {
			title = title[:60] + "..."
		}
		s.Meta.Title = title
	}
	ss.rewriteMeta(s)
	return nil
}

// Rename changes the title of a session.
func (ss *SessionStore) Rename(id, title string) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	s, err := ss.loadLocked(id)
	if err != nil {
		return err
	}
	s.Meta.Title = title
	s.Meta.UpdatedAt = time.Now()
	ss.rewriteMeta(s)
	return nil
}

// UpdateConsolidated updates the consolidation pointer.
func (ss *SessionStore) UpdateConsolidated(id string, idx int) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	s, err := ss.loadLocked(id)
	if err != nil {
		return
	}
	s.Meta.LastConsolidated = idx
	ss.rewriteMeta(s)
}

// Unconsolidated returns messages after the last_consolidated pointer.
func (ss *SessionStore) Unconsolidated(id string) ([]SessionMessage, int) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	s, err := ss.loadLocked(id)
	if err != nil {
		return nil, 0
	}
	start := s.Meta.LastConsolidated
	if start >= len(s.Messages) {
		return nil, start
	}
	return s.Messages[start:], start
}

// RecentMessages returns the last n messages from a session.
func (ss *SessionStore) RecentMessages(id string, n int) []SessionMessage {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	s, err := ss.loadLocked(id)
	if err != nil {
		return nil
	}
	if n >= len(s.Messages) {
		return s.Messages
	}
	return s.Messages[len(s.Messages)-n:]
}

// List returns all session metadata sorted by most recent.
func (ss *SessionStore) List() []SessionMeta {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	entries, err := os.ReadDir(ss.dir)
	if err != nil {
		return nil
	}

	var metas []SessionMeta
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".jsonl")
		s, err := ss.loadLocked(id)
		if err != nil {
			continue
		}
		metas = append(metas, s.Meta)
	}

	sort.Slice(metas, func(i, j int) bool {
		return metas[i].UpdatedAt.After(metas[j].UpdatedAt)
	})
	return metas
}

// Delete removes a session file from disk.
func (ss *SessionStore) Delete(id string) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return os.Remove(ss.sessionPath(id))
}

func (ss *SessionStore) sessionPath(id string) string {
	return filepath.Join(ss.dir, id+".jsonl")
}

func (ss *SessionStore) writeMeta(meta SessionMeta) {
	data, _ := json.Marshal(meta)
	os.WriteFile(ss.sessionPath(meta.ID), append(data, '\n'), 0644)
}

// rewriteMeta rewrites the entire JSONL file with updated metadata.
func (ss *SessionStore) rewriteMeta(s *Session) {
	path := ss.sessionPath(s.Meta.ID)

	var lines [][]byte
	metaData, _ := json.Marshal(s.Meta)
	lines = append(lines, metaData)

	for _, msg := range s.Messages {
		data, _ := json.Marshal(msg)
		lines = append(lines, data)
	}

	var buf strings.Builder
	for _, line := range lines {
		buf.Write(line)
		buf.WriteByte('\n')
	}
	os.WriteFile(path, []byte(buf.String()), 0644)
}

func randomID(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
