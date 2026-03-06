package chat

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ddx-510/Morpho/agent"
	"github.com/ddx-510/Morpho/config"
	"github.com/ddx-510/Morpho/domain"
	"github.com/ddx-510/Morpho/event"
	"github.com/ddx-510/Morpho/llm"
	"github.com/ddx-510/Morpho/memory"
	"github.com/ddx-510/Morpho/tool"
)

// Message is a chat message in the conversation history.
type Message struct {
	Role     string        `json:"role"`
	Content  string        `json:"content"`
	Meta     string        `json:"meta,omitempty"`
	Strategy string        `json:"strategy,omitempty"`
	Steps    []memory.Step `json:"steps,omitempty"`
}

// App is the core morpho chat application.
type App struct {
	Provider llm.Provider
	Cfg      *config.Config
	WorkDir  string
	Bus      *event.Bus
	Router   *Router
	Cron     *Cron
	Facts    *memory.FactStore
	Sessions *memory.SessionStore
	Tissue   *memory.TissueMemory

	session *memory.Session
	history []Message
	soul    string
	steps   []memory.Step
	mu      sync.Mutex
}

// New creates a new chat app with memory stores initialized.
func New(provider llm.Provider, cfg *config.Config, workDir string) *App {
	bus := event.NewBus()
	facts := memory.NewFactStore(cfg.Memory.FactsPath)
	sessions := memory.NewSessionStore(cfg.Memory.SessionDir)
	tissue := memory.NewTissueMemory(filepath.Join(filepath.Dir(cfg.Memory.FactsPath), "tissue.json"))

	app := &App{
		Provider: provider,
		Cfg:      cfg,
		WorkDir:  workDir,
		Bus:      bus,
		Router:   newRouter(provider),
		Facts:    facts,
		Sessions: sessions,
		Tissue:   tissue,
		history:  []Message{},
	}

	app.session = sessions.Create()

	if data, err := os.ReadFile(filepath.Join(workDir, "SOUL.md")); err == nil {
		app.soul = string(data)
	}

	app.Cron = newCron(func(job *CronJob) {
		app.Bus.Emit(event.Event{
			Type:    event.Thinking,
			Content: fmt.Sprintf("cron: running scheduled job [%s] %s", job.ID, job.Task),
		})
		reply := app.HandleMessage(job.Task)
		app.Bus.Emit(event.Event{Type: event.AssistantMessage, Content: fmt.Sprintf("[cron:%s] %s", job.ID, reply)})
	})

	return app
}

// SessionID returns the current session ID.
func (app *App) SessionID() string {
	return app.session.ID()
}

// LoadSession switches to an existing session, restoring its history.
func (app *App) LoadSession(id string) error {
	s, err := app.Sessions.Load(id)
	if err != nil {
		return err
	}
	app.mu.Lock()
	defer app.mu.Unlock()

	app.session = s
	app.history = make([]Message, len(s.Messages))
	for i, m := range s.Messages {
		app.history[i] = Message{Role: m.Role, Content: m.Content, Strategy: m.Strategy, Steps: m.Steps}
	}
	return nil
}

// NewSession creates a fresh session.
func (app *App) NewSession() string {
	app.mu.Lock()
	defer app.mu.Unlock()

	app.session = app.Sessions.Create()
	app.history = []Message{}
	return app.session.ID()
}

// History returns a copy of the conversation history.
func (app *App) History() []Message {
	app.mu.Lock()
	defer app.mu.Unlock()
	out := make([]Message, len(app.history))
	copy(out, app.history)
	return out
}

// HandleMessage classifies a message and routes to the appropriate tier.
func (app *App) HandleMessage(msg string) string {
	app.mu.Lock()
	app.history = append(app.history, Message{Role: "user", Content: msg})
	history := app.routerHistory()
	app.mu.Unlock()

	app.Sessions.Append(app.session.ID(), memory.SessionMessage{
		Role: "user", Content: msg,
	})

	plan := app.Router.Classify(msg, history)

	app.mu.Lock()
	app.steps = nil
	app.mu.Unlock()

	app.Bus.Emit(event.Event{
		Type:    event.Thinking,
		Content: fmt.Sprintf("[%s] %s", plan.Strategy, plan.Reason),
	})

	var reply string
	var strategy string
	switch plan.Strategy {
	case SwarmTier:
		strategy = "swarm"
		task := plan.Task
		if task == "" {
			task = msg
		}
		reply = app.runSwarm(task)
	case AssistTier:
		strategy = "assist"
		task := plan.Task
		if task == "" {
			task = msg
		}
		reply = app.runAssist(msg, task)
	default:
		strategy = "chat"
		reply = app.runChat(msg)
	}

	app.mu.Lock()
	steps := make([]memory.Step, len(app.steps))
	copy(steps, app.steps)
	app.steps = nil
	app.history = append(app.history, Message{Role: "assistant", Content: reply, Strategy: strategy, Steps: steps})
	app.mu.Unlock()

	app.Sessions.Append(app.session.ID(), memory.SessionMessage{
		Role: "assistant", Content: reply, Strategy: strategy, Steps: steps,
	})

	go app.extractFacts(msg, reply, strategy)
	go app.maybeConsolidate()

	return reply
}

// HandleCommand processes slash commands.
func (app *App) HandleCommand(input string) string {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return ""
	}
	switch parts[0] {
	case "/help":
		return helpText
	case "/cron":
		return app.handleCronCommand(parts[1:])
	case "/session":
		return app.handleSessionCommand(parts[1:])
	case "/facts":
		return app.handleFactsCommand(parts[1:])
	case "/remember":
		if len(parts) < 2 {
			return "usage: /remember <fact>"
		}
		fact := strings.Join(parts[1:], " ")
		app.Facts.Add("user:explicit", fact, "user")
		return fmt.Sprintf("remembered: %s", fact)
	case "/forget":
		if len(parts) < 2 {
			return "usage: /forget <fact-id>"
		}
		app.Facts.Remove(parts[1])
		return fmt.Sprintf("forgot fact %s", parts[1])
	default:
		return fmt.Sprintf("unknown command: %s — type /help for all commands", parts[0])
	}
}

const helpText = `morpho — multi-agent assistant with memory

ROUTING
  Messages are auto-classified into one of three tiers:
    chat    Direct LLM response for simple messages
    assist  Single agent with tools (read, edit, grep, shell)
    swarm   Multi-agent morphogenetic system for broad analysis

SESSIONS
  /session              Show current session
  /session new          Start a new conversation
  /session list         List all saved sessions
  /session load <id>    Resume a previous session
  /session rename <id> <name>  Rename a session

MEMORY
  /facts                Show fact count
  /facts list           List all stored facts
  /facts search <q>     Search facts by keyword
  /facts rm <id>        Delete a fact
  /remember <text>      Manually store a fact
  /forget <id>          Delete a fact by ID

CRON
  /cron add <interval> <task>  Schedule a recurring task (e.g. 5m, 1h)
  /cron list                   List scheduled jobs
  /cron rm <id>                Remove a job
  /cron pause <id>             Pause a job
  /cron resume <id>            Resume a paused job

OTHER
  /help                 Show this help
  /quit                 Exit (terminal only)`

// ── Fact context builder ────────────────────────────────────────────

func (app *App) relevantFacts(msg string) string {
	facts := app.Facts.Relevant(msg, 10)
	if len(facts) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\nRelevant knowledge from memory:\n")
	for _, f := range facts {
		fmt.Fprintf(&sb, "- [%s] %s\n", f.Topic, f.Content)
	}
	return sb.String()
}

func (app *App) extractFacts(userMsg, reply, strategy string) {
	if strategy == "chat" && len(reply) < 100 {
		return
	}

	resp := app.Provider.Generate(llm.Request{
		SystemPrompt: `Extract reusable facts from this conversation exchange. Facts should be things worth remembering for future conversations — patterns, preferences, project details, important findings.

Return ONLY a JSON array of objects: [{"topic": "category:subcategory", "content": "the fact"}]
Topic categories: project, code, user, pattern, finding, config
If nothing is worth remembering, return [].`,
		UserPrompt: fmt.Sprintf("User: %s\n\nAssistant (%s): %s", userMsg, strategy, truncate(reply, 1000)),
	})
	if resp.Err != nil {
		return
	}

	type factJSON struct {
		Topic   string `json:"topic"`
		Content string `json:"content"`
	}

	raw := strings.TrimSpace(resp.Content)
	if idx := strings.Index(raw, "["); idx >= 0 {
		if end := strings.LastIndex(raw, "]"); end > idx {
			raw = raw[idx : end+1]
		}
	}

	var extracted []factJSON
	if err := parseJSON(raw, &extracted); err != nil {
		return
	}

	for _, f := range extracted {
		if f.Topic != "" && f.Content != "" {
			app.Facts.Add(f.Topic, f.Content, strategy)
		}
	}
}

func (app *App) routerHistory() []RouterMessage {
	start := 0
	if len(app.history) > 10 {
		start = len(app.history) - 10
	}
	out := make([]RouterMessage, 0, len(app.history)-start)
	for _, m := range app.history[start:] {
		if m.Role == "user" || m.Role == "assistant" {
			out = append(out, RouterMessage{Role: m.Role, Content: m.Content})
		}
	}
	return out
}

// ── Session commands ────────────────────────────────────────────────

func (app *App) handleSessionCommand(args []string) string {
	if len(args) == 0 {
		return fmt.Sprintf("current session: %s\nusage: /session new | list | load <id> | rename <id> <name>", app.session.ID())
	}
	switch args[0] {
	case "new":
		id := app.NewSession()
		return fmt.Sprintf("started new session: %s", id)
	case "list":
		metas := app.Sessions.List()
		if len(metas) == 0 {
			return "no saved sessions"
		}
		var sb strings.Builder
		for _, m := range metas {
			title := m.Title
			if title == "" {
				title = "(untitled)"
			}
			marker := "  "
			if m.ID == app.session.ID() {
				marker = "* "
			}
			fmt.Fprintf(&sb, "  %s%s  %s  %d msgs  %s\n", marker, m.ID, m.UpdatedAt.Format("Jan 02 15:04"), m.MessageCount, title)
		}
		return sb.String()
	case "load":
		if len(args) < 2 {
			return "usage: /session load <id>"
		}
		if err := app.LoadSession(args[1]); err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		return fmt.Sprintf("loaded session %s (%d messages)", args[1], len(app.history))
	case "rename":
		if len(args) < 3 {
			return "usage: /session rename <id> <name>"
		}
		name := strings.Join(args[2:], " ")
		if err := app.Sessions.Rename(args[1], name); err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		return fmt.Sprintf("renamed session %s to: %s", args[1], name)
	default:
		return "usage: /session new | list | load <id> | rename <id> <name>"
	}
}

// ── Facts commands ──────────────────────────────────────────────────

func (app *App) handleFactsCommand(args []string) string {
	if len(args) == 0 {
		return fmt.Sprintf("%d facts stored\nusage: /facts list | search <query> | rm <id>", app.Facts.Count())
	}
	switch args[0] {
	case "list":
		facts := app.Facts.All()
		if len(facts) == 0 {
			return "no facts stored"
		}
		var sb strings.Builder
		for _, f := range facts {
			fmt.Fprintf(&sb, "  %s  [%s] %s  (from %s)\n", f.ID, f.Topic, f.Content, f.Source)
		}
		return sb.String()
	case "search":
		if len(args) < 2 {
			return "usage: /facts search <query>"
		}
		query := strings.Join(args[1:], " ")
		facts := app.Facts.Search(query)
		if len(facts) == 0 {
			return "no matching facts"
		}
		var sb strings.Builder
		for _, f := range facts {
			fmt.Fprintf(&sb, "  %s  [%s] %s\n", f.ID, f.Topic, f.Content)
		}
		return sb.String()
	case "rm", "remove":
		if len(args) < 2 {
			return "usage: /facts rm <id>"
		}
		app.Facts.Remove(args[1])
		return fmt.Sprintf("removed fact %s", args[1])
	default:
		return "usage: /facts list | search <query> | rm <id>"
	}
}

// ── Cron commands ───────────────────────────────────────────────────

var cronIDCounter int

func (app *App) handleCronCommand(args []string) string {
	if len(args) == 0 {
		return "usage: /cron add <interval> <task> | list | rm <id> | pause <id> | resume <id>"
	}
	switch args[0] {
	case "add":
		if len(args) < 3 {
			return "usage: /cron add <interval> <task>\n  interval: 30s, 5m, 1h, 24h"
		}
		dur, err := time.ParseDuration(args[1])
		if err != nil {
			return fmt.Sprintf("invalid interval %q: %v", args[1], err)
		}
		if dur < 10*time.Second {
			return "minimum interval is 10s"
		}
		cronIDCounter++
		id := fmt.Sprintf("job%d", cronIDCounter)
		task := strings.Join(args[2:], " ")
		app.Cron.Add(&CronJob{ID: id, Task: task, Interval: dur})
		return fmt.Sprintf("scheduled [%s] every %s: %s", id, dur, task)
	case "list":
		jobs := app.Cron.List()
		if len(jobs) == 0 {
			return "no scheduled jobs"
		}
		var sb strings.Builder
		for _, j := range jobs {
			status := "active"
			if !j.Enabled {
				status = "paused"
			}
			fmt.Fprintf(&sb, "  [%s] every %s (%s): %s\n", j.ID, j.Interval, status, j.Task)
		}
		return sb.String()
	case "rm", "remove", "delete":
		if len(args) < 2 {
			return "usage: /cron rm <id>"
		}
		app.Cron.Remove(args[1])
		return fmt.Sprintf("removed job %s", args[1])
	case "pause":
		if len(args) < 2 {
			return "usage: /cron pause <id>"
		}
		app.Cron.Pause(args[1])
		return fmt.Sprintf("paused job %s", args[1])
	case "resume":
		if len(args) < 2 {
			return "usage: /cron resume <id>"
		}
		app.Cron.Resume(args[1])
		return fmt.Sprintf("resumed job %s", args[1])
	default:
		return "usage: /cron add <interval> <task> | list | rm <id> | pause <id> | resume <id>"
	}
}

// ── Chat tier ───────────────────────────────────────────────────────

func (app *App) runChat(msg string) string {
	var histCtx strings.Builder
	app.mu.Lock()
	start := 0
	if len(app.history) > 10 {
		start = len(app.history) - 10
	}
	for _, m := range app.history[start:] {
		fmt.Fprintf(&histCtx, "%s: %s\n", m.Role, m.Content)
	}
	app.mu.Unlock()

	factsCtx := app.relevantFacts(msg)

	soulCtx := app.soulSummary()
	resp := app.Provider.Generate(llm.Request{
		SystemPrompt: fmt.Sprintf(`%s
Working directory: %s
Provider: %s
%s
Recent conversation:
%s`, soulCtx, app.WorkDir, app.Provider.Name(), factsCtx, histCtx.String()),
		UserPrompt: msg,
	})
	if resp.Err != nil {
		return fmt.Sprintf("error: %v", resp.Err)
	}
	return resp.Content
}

// ── Assist tier ─────────────────────────────────────────────────────

const maxAssistTurns = 30

func (app *App) runAssist(msg, task string) string {
	targetDir := resolveTargetDir(msg+" "+task, app.WorkDir)
	tools := tool.DefaultRegistry(targetDir)
	tool.RegisterSkills(tools, app.Provider)

	var histCtx strings.Builder
	app.mu.Lock()
	start := 0
	if len(app.history) > 6 {
		start = len(app.history) - 6
	}
	for _, m := range app.history[start:] {
		fmt.Fprintf(&histCtx, "%s: %s\n", m.Role, m.Content)
	}
	app.mu.Unlock()

	factsCtx := app.relevantFacts(msg + " " + task)

	soulCtx := app.soulSummary()
	systemPrompt := fmt.Sprintf(`%s
Working directory: %s
%s
Recent conversation:
%s

Task: %s

You have access to tools: read_file, edit_file, grep, shell, list_files, and skills.
All tools accept both absolute paths and relative paths (resolved from working directory).
Use list_files to explore directories. Use read_file to read files. Use grep to search.
Start by listing files in the relevant directory to understand the structure.
When you have enough information, respond directly with your answer.
Be concise and specific. Cite file paths and line numbers when relevant.`, soulCtx, targetDir, factsCtx, histCtx.String(), task)

	// Build proper multi-turn conversation
	messages := []llm.ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: msg},
	}

	toolSpecs := tools.ToLLMSpecs()
	callCounter := 0

	for turn := 0; turn < maxAssistTurns; turn++ {
		resp := app.Provider.Generate(llm.Request{
			Messages: messages,
			Tools:    toolSpecs,
		})
		if resp.Err != nil {
			return fmt.Sprintf("error: %v", resp.Err)
		}
		if len(resp.ToolCalls) == 0 {
			if resp.Content == "" {
				return "(no response)"
			}
			return resp.Content
		}

		// Append assistant message with tool calls
		messages = append(messages, llm.ChatMessage{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		for _, call := range resp.ToolCalls {
			argsStr := fmt.Sprintf("%v", call.Args)
			app.Bus.Emit(event.Event{
				Type: event.ToolUse,
				Meta: map[string]string{
					"tool":  call.Name,
					"args":  argsStr,
					"agent": "assist",
				},
			})
			app.addStep(memory.Step{Kind: "tool_use", Agent: "assist", Tool: call.Name, Args: truncate(argsStr, 200)})

			callID := call.ID
			if callID == "" {
				callCounter++
				callID = fmt.Sprintf("call_%d", callCounter)
			}

			result := tools.ExecuteCall(call)
			var resultStr string
			if result.Err != nil {
				resultStr = fmt.Sprintf("error: %v", result.Err)
			} else {
				resultStr = result.Output
				if len(resultStr) > 4000 {
					resultStr = resultStr[:4000] + "\n... (truncated)"
				}
			}

			resultPreview := truncate(resultStr, 200)
			app.addStep(memory.Step{Kind: "tool_result", Agent: "assist", Tool: call.Name, Content: resultPreview})
			app.Bus.Emit(event.Event{
				Type:    event.ToolResult,
				Content: resultPreview,
				Meta:    map[string]string{"tool": call.Name, "agent": "assist"},
			})

			// Append tool result as proper message
			messages = append(messages, llm.ChatMessage{
				Role:       "tool",
				Content:    resultStr,
				ToolCallID: callID,
			})
		}
	}
	return "(assist reached turn limit)"
}

// ── Swarm tier ──────────────────────────────────────────────────────

func (app *App) runSwarm(msg string) string {
	targetDir := resolveTargetDir(msg, app.WorkDir)

	dom, err := domain.Auto(app.Provider, msg, targetDir)
	if err != nil {
		return fmt.Sprintf("failed to generate domain: %v", err)
	}
	app.Bus.Emit(event.Event{Type: event.Thinking, Content: fmt.Sprintf("domain: %s — %s (target: %s)", dom.Name, dom.Description, targetDir)})

	f, err := dom.Seeder(targetDir)
	if err != nil {
		return fmt.Sprintf("seed error: %v", err)
	}
	points := f.Points()
	if len(points) == 0 {
		return fmt.Sprintf("No content found to analyze in %s.", targetDir)
	}

	// Inject tissue memory: residual signals and prior findings from past runs.
	for _, pid := range points {
		priorSigs, priorFindings := app.Tissue.Recall(pid)
		for sig, val := range priorSigs {
			f.AddSignal(pid, agent.Signal(sig), val*0.5) // dampen residual signals
		}
		for _, finding := range priorFindings {
			f.DepositFinding(pid, "[prior] "+finding)
		}
	}

	roles := agent.NewRoleMapping()
	for _, r := range dom.Roles {
		roles.SignalToRole[r.Signal] = r.Name
		roles.RoleToSignal[r.Name] = r.Signal
		roles.RolePrompts[r.Name] = r.Prompt
	}

	tools := dom.ToolBuilder(targetDir)
	tool.RegisterSkills(tools, app.Provider)
	for _, mcpCfg := range app.Cfg.MCPServers {
		tool.RegisterMCPTools(tools, tool.MCPServerConfig{
			Name:    mcpCfg.Name,
			Command: mcpCfg.Command,
			Args:    mcpCfg.Args,
			Env:     mcpCfg.Env,
		})
	}

	engCfg := agent.EngineConfig{
		MaxTicks:      app.Cfg.Engine.MaxTicks,
		DecayRate:     app.Cfg.Engine.DecayRate,
		DiffusionRate: app.Cfg.Engine.DiffusionRate,
		Provider:      app.Provider,
	}

	eng := agent.NewEngine(f, engCfg, tools, roles)
	eng.Quiet()

	factsCtx := app.relevantFacts(msg)
	if factsCtx != "" {
		eng.SetContextHint(factsCtx)
	}

	start := time.Now()
	eng.SetProgress(func(ev agent.ProgressEvent) {
		switch ev.Kind {
		case "tick":
			app.Bus.Emit(event.Event{
				Type: event.TickStart,
				Meta: map[string]string{"tick": fmt.Sprintf("%d/%d", ev.Tick, ev.Total)},
			})
			app.addStep(memory.Step{Kind: "tick", Content: fmt.Sprintf("tick %d/%d", ev.Tick, ev.Total)})
		case "spawn":
			app.Bus.Emit(event.Event{
				Type: event.AgentSpawn,
				Meta: map[string]string{"agent": ev.Agent, "point": ev.Point},
			})
			app.addStep(memory.Step{Kind: "agent_spawn", Agent: ev.Agent, Point: ev.Point})
		case "differentiate":
			app.Bus.Emit(event.Event{
				Type: event.AgentDiff,
				Meta: map[string]string{"agent": ev.Agent, "role": ev.Role, "point": ev.Point},
			})
			app.addStep(memory.Step{Kind: "agent_diff", Agent: ev.Agent, Role: ev.Role, Point: ev.Point})
		case "move":
			app.Bus.Emit(event.Event{
				Type: event.AgentMove,
				Meta: map[string]string{"agent": ev.Agent, "role": ev.Role, "point": ev.Point},
			})
			app.addStep(memory.Step{Kind: "agent_move", Agent: ev.Agent, Role: ev.Role, Point: ev.Point, Content: ev.Detail})
		case "work_done":
			app.Bus.Emit(event.Event{
				Type:    event.AgentDone,
				Content: ev.Detail,
				Meta:    map[string]string{"agent": ev.Agent, "role": ev.Role, "point": ev.Point, "tokens": fmt.Sprintf("%d", ev.Tokens)},
			})
			app.addStep(memory.Step{Kind: "agent_done", Agent: ev.Agent, Role: ev.Role, Point: ev.Point, Content: truncate(ev.Detail, 500)})
		case "tool_use":
			toolName := ev.Detail
			toolArgs := ""
			if idx := strings.Index(ev.Detail, ": "); idx > 0 {
				toolName = ev.Detail[:idx]
				toolArgs = ev.Detail[idx+2:]
			}
			app.Bus.Emit(event.Event{
				Type: event.ToolUse,
				Meta: map[string]string{"tool": toolName, "args": ev.Detail, "agent": ev.Agent, "point": ev.Point, "role": ev.Role},
			})
			app.addStep(memory.Step{Kind: "tool_use", Agent: ev.Agent, Role: ev.Role, Point: ev.Point, Tool: toolName, Args: truncate(toolArgs, 200)})
		case "tool_result":
			app.Bus.Emit(event.Event{
				Type:    event.ToolResult,
				Content: ev.Detail,
				Meta:    map[string]string{"agent": ev.Agent, "point": ev.Point, "role": ev.Role},
			})
			app.addStep(memory.Step{Kind: "tool_result", Agent: ev.Agent, Role: ev.Role, Point: ev.Point, Content: truncate(ev.Detail, 300)})
		case "apoptosis":
			app.Bus.Emit(event.Event{
				Type: event.AgentDeath,
				Meta: map[string]string{"agent": ev.Agent, "role": ev.Role},
			})
			app.addStep(memory.Step{Kind: "agent_death", Agent: ev.Agent, Role: ev.Role})
		case "field_state":
			if regionJSON, err := json.Marshal(ev.Regions); err == nil {
				app.Bus.Emit(event.Event{
					Type:    event.FieldState,
					Content: string(regionJSON),
					Meta:    map[string]string{"tick": fmt.Sprintf("%d", ev.Tick)},
				})
			}
		case "complete":
			app.Bus.Emit(event.Event{
				Type: event.RunComplete,
				Meta: map[string]string{
					"findings": fmt.Sprintf("%d", ev.Finding),
					"elapsed":  time.Since(start).Round(time.Millisecond).String(),
					"tokens":   fmt.Sprintf("%d", ev.Tokens),
				},
			})
			app.addStep(memory.Step{Kind: "complete", Content: fmt.Sprintf("%d findings in %s", ev.Finding, time.Since(start).Round(time.Millisecond))})
		}
	})

	result := eng.Run()

	// Absorb field state into tissue memory (epigenetic persistence).
	for _, pid := range eng.Field.Points() {
		pt, ok := eng.Field.FieldPoint(pid)
		if !ok {
			continue
		}
		sigs := make(map[string]float64)
		for k, v := range pt.Signals {
			sigs[string(k)] = v
		}
		app.Tissue.Absorb(pid, sigs, pt.Findings)
	}

	for i, finding := range result.Findings {
		if i >= 5 {
			break
		}
		app.Facts.Add("finding:swarm", truncate(finding, 200), "swarm")
	}

	if len(result.Findings) == 0 {
		return "Analysis complete — no significant findings."
	}

	var findingSummary strings.Builder
	for i, f := range result.Findings {
		if i >= 20 {
			fmt.Fprintf(&findingSummary, "... and %d more findings\n", len(result.Findings)-20)
			break
		}
		fmt.Fprintf(&findingSummary, "%d. %s\n", i+1, f)
	}

	resp := app.Provider.Generate(llm.Request{
		SystemPrompt: `You are Morpho, summarizing findings from a multi-agent analysis.
Organize the findings by category. Use markdown. Be concise but thorough.
Include the most important/critical findings first.`,
		UserPrompt: fmt.Sprintf("Task: %s\n\nRaw findings from %d specialist agents across %d regions in %s:\n\n%s",
			msg, len(result.Findings), len(result.ByPoint), result.Duration.Round(time.Millisecond), findingSummary.String()),
	})
	if resp.Err != nil {
		return agent.PrintReport(result)
	}
	return resp.Content
}

// addStep records an intermediate step for the current turn.
func (app *App) addStep(s memory.Step) {
	app.mu.Lock()
	app.steps = append(app.steps, s)
	app.mu.Unlock()
}

func (app *App) soulSummary() string {
	if app.soul != "" {
		s := app.soul
		if len(s) > 800 {
			s = s[:800]
		}
		return s
	}
	return "You are Morpho, an adaptive multi-agent assistant. You are helpful, concise, and direct."
}

func resolveTargetDir(msg, defaultDir string) string {
	for _, word := range strings.Fields(msg) {
		word = strings.Trim(word, "\"'`(),;:!?")
		if word == "" {
			continue
		}

		if strings.HasPrefix(word, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				expanded := filepath.Join(home, word[2:])
				if info, err := os.Stat(expanded); err == nil && info.IsDir() {
					return expanded
				}
			}
			continue
		}

		if filepath.IsAbs(word) {
			if info, err := os.Stat(word); err == nil && info.IsDir() {
				return word
			}
			continue
		}

		candidate := filepath.Join(defaultDir, word)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}

		if home, err := os.UserHomeDir(); err == nil {
			candidate = filepath.Join(home, word)
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				return candidate
			}
			candidate = filepath.Join(home, "Desktop", word)
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				return candidate
			}
			if entries, err := os.ReadDir(filepath.Join(home, "Desktop")); err == nil {
				for _, e := range entries {
					if !e.IsDir() {
						continue
					}
					candidate = filepath.Join(home, "Desktop", e.Name(), word)
					if info, err := os.Stat(candidate); err == nil && info.IsDir() {
						return candidate
					}
				}
			}
		}
	}
	return defaultDir
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func parseJSON(raw string, v any) error {
	return json.Unmarshal([]byte(raw), v)
}

// ── Consolidation ──────────────────────────────────────────────────

const consolidateThreshold = 10

func (app *App) maybeConsolidate() {
	msgs, startIdx := app.Sessions.Unconsolidated(app.session.ID())
	if len(msgs) < consolidateThreshold {
		return
	}

	var conv strings.Builder
	for _, m := range msgs {
		fmt.Fprintf(&conv, "%s: %s\n", m.Role, truncate(m.Content, 500))
	}

	resp := app.Provider.Generate(llm.Request{
		SystemPrompt: `You are a memory consolidation agent. Extract reusable knowledge from this conversation segment.

Focus on:
- Project facts, code patterns, architectural decisions
- User preferences and corrections
- Important findings from analysis
- Anything worth remembering for future conversations

Return ONLY a JSON array: [{"topic": "category:detail", "content": "the fact"}]
Topic categories: project, code, user, pattern, finding, config, decision
If nothing is worth remembering, return [].
Be selective — only extract genuinely useful, non-obvious facts.`,
		UserPrompt: conv.String(),
	})
	if resp.Err != nil {
		return
	}

	type factJSON struct {
		Topic   string `json:"topic"`
		Content string `json:"content"`
	}

	raw := strings.TrimSpace(resp.Content)
	if idx := strings.Index(raw, "["); idx >= 0 {
		if end := strings.LastIndex(raw, "]"); end > idx {
			raw = raw[idx : end+1]
		}
	}

	var extracted []factJSON
	if err := parseJSON(raw, &extracted); err != nil {
		return
	}

	for _, f := range extracted {
		if f.Topic != "" && f.Content != "" {
			app.Facts.Add(f.Topic, f.Content, "consolidation")
		}
	}

	app.Sessions.UpdateConsolidated(app.session.ID(), startIdx+len(msgs))

	if app.Facts.NeedsCompression() {
		app.compressFacts()
	}
}

func (app *App) compressFacts() {
	allFacts := app.Facts.Dump()
	if allFacts == "" {
		return
	}

	resp := app.Provider.Generate(llm.Request{
		SystemPrompt: `You are a memory compression agent. The fact store has grown too large.
Merge duplicate facts, remove outdated ones, and consolidate related items.
Keep the most important and frequently accessed facts.

Return ONLY a JSON array: [{"topic": "category:detail", "content": "the fact"}]
Aim to reduce the count by ~40% while preserving all critical knowledge.`,
		UserPrompt: fmt.Sprintf("Current facts (%d total):\n%s", app.Facts.Count(), allFacts),
	})
	if resp.Err != nil {
		return
	}

	type factJSON struct {
		Topic   string `json:"topic"`
		Content string `json:"content"`
	}

	raw := strings.TrimSpace(resp.Content)
	if idx := strings.Index(raw, "["); idx >= 0 {
		if end := strings.LastIndex(raw, "]"); end > idx {
			raw = raw[idx : end+1]
		}
	}

	var compressed []factJSON
	if err := parseJSON(raw, &compressed); err != nil {
		return
	}

	var newFacts []memory.Fact
	now := time.Now()
	for _, f := range compressed {
		if f.Topic != "" && f.Content != "" {
			newFacts = append(newFacts, memory.Fact{
				ID:        fmt.Sprintf("%x", now.UnixNano())[:12],
				Topic:     f.Topic,
				Content:   f.Content,
				Source:    "compressed",
				CreatedAt: now,
				UpdatedAt: now,
			})
		}
	}

	if len(newFacts) > 0 {
		app.Facts.ReplaceAll(newFacts)
	}
}
