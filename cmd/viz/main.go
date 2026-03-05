package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/ddx-510/Morpho/agent"
	"github.com/ddx-510/Morpho/config"
	"github.com/ddx-510/Morpho/domain"
	"github.com/ddx-510/Morpho/engine"
	"github.com/ddx-510/Morpho/llm"
	"github.com/ddx-510/Morpho/memory"
)

// sseHub manages Server-Sent Event connections.
type sseHub struct {
	mu      sync.Mutex
	clients map[chan []byte]bool
}

func newHub() *sseHub {
	return &sseHub{clients: make(map[chan []byte]bool)}
}

func (h *sseHub) add() chan []byte {
	ch := make(chan []byte, 64)
	h.mu.Lock()
	h.clients[ch] = true
	h.mu.Unlock()
	return ch
}

func (h *sseHub) remove(ch chan []byte) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
	close(ch)
}

func (h *sseHub) broadcast(data []byte) {
	h.mu.Lock()
	for ch := range h.clients {
		select {
		case ch <- data:
		default: // drop if buffer full
		}
	}
	h.mu.Unlock()
}

// vizEvent is sent to the browser via SSE.
type vizEvent struct {
	Type      string            `json:"type"`
	Tick      int               `json:"tick,omitempty"`
	MaxTicks  int               `json:"maxTicks,omitempty"`
	Agent     string            `json:"agent,omitempty"`
	Role      string            `json:"role,omitempty"`
	Point     string            `json:"point,omitempty"`
	Detail    string            `json:"detail,omitempty"`
	Done      int               `json:"done,omitempty"`
	Total     int               `json:"total,omitempty"`
	Findings  int               `json:"findings,omitempty"`
	Elapsed   string            `json:"elapsed,omitempty"`
	Field     map[string]any    `json:"field,omitempty"`
	Regions   []string          `json:"regions,omitempty"`
	ByRole    map[string]int    `json:"byRole,omitempty"`
	ByPoint   map[string]int    `json:"byPoint,omitempty"`
	Tissues   []string          `json:"tissues,omitempty"`
	FindItems []string          `json:"findItems,omitempty"`
}

var (
	hub       = newHub()
	startTime time.Time
)

func main() {
	configPath := flag.String("config", "morpho.json", "Path to config file")
	port := flag.String("port", "8420", "HTTP port")
	domainName := flag.String("domain", "code_review", "Domain: code_review, research, writing_review, data_analysis, or free-text task")
	flag.Parse()

	target := "."
	if flag.NArg() > 0 {
		target = flag.Arg(0)
	}
	if target == "." {
		target, _ = os.Getwd()
	}

	info, err := os.Stat(target)
	if err != nil {
		log.Fatalf("cannot access %s: %v", target, err)
	}
	if !info.IsDir() {
		log.Fatalf("%s is not a directory", target)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	provider, err := cfg.BuildProvider()
	if err != nil {
		log.Fatalf("provider: %v", err)
	}

	// Resolve domain.
	var dom *domain.Domain
	if d, ok := domain.Get(*domainName); ok {
		dom = d
	} else {
		d, err := domain.Auto(provider, *domainName, target)
		if err != nil {
			log.Fatalf("auto domain: %v", err)
		}
		dom = d
	}

	var running sync.Mutex

	// HTTP routes
	http.HandleFunc("/", serveUI)
	http.HandleFunc("/events", serveSSE)
	http.HandleFunc("/run", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !running.TryLock() {
			json.NewEncoder(w).Encode(map[string]string{"status": "already_running"})
			return
		}
		go func() {
			defer running.Unlock()
			runMorpho(target, cfg, provider, dom)
		}()
		json.NewEncoder(w).Encode(map[string]string{"status": "started"})
	})

	addr := ":" + *port
	fmt.Printf("Morpho Viz → http://localhost%s\n", addr)
	fmt.Printf("Target: %s\n", target)
	fmt.Printf("Provider: %s\n", provider.Name())
	fmt.Println("Open in browser, then click Start.")
	log.Fatal(http.ListenAndServe(addr, nil))
}

func runMorpho(target string, cfg *config.Config, provider llm.Provider, dom *domain.Domain) {
	startTime = time.Now()

	f, err := dom.Seeder(target)
	if err != nil {
		emit(vizEvent{Type: "complete", Detail: "seed error: " + err.Error()})
		return
	}

	regions := f.Points()
	if len(regions) == 0 {
		emit(vizEvent{Type: "complete", Detail: "no content found"})
		return
	}

	// Build field snapshot for init event.
	fieldData := make(map[string]any)
	for _, pid := range regions {
		pt, ok := f.Point(pid)
		if ok {
			sigs := make(map[string]float64)
			for sig, val := range pt.Signals {
				if val > 0.01 {
					sigs[string(sig)] = val
				}
			}
			fieldData[pid] = sigs
		}
	}
	emit(vizEvent{Type: "init", Regions: regions, Field: fieldData})

	// Build role mapping from domain.
	roles := agent.NewRoleMapping()
	for _, r := range dom.Roles {
		roles.SignalToRole[r.Signal] = r.Name
		roles.RoleToSignal[r.Name] = r.Signal
		roles.RolePrompts[r.Name] = r.Prompt
	}

	tools := dom.ToolBuilder(target)
	longMem := memory.NewLongTerm("")

	engCfg := engine.Config{
		MaxTicks:          cfg.Engine.MaxTicks,
		DecayRate:         cfg.Engine.DecayRate,
		DiffusionRate:     cfg.Engine.DiffusionRate,
		SpawnPerTick:      cfg.Engine.SpawnPerTick,
		ShortTermCapacity: cfg.Memory.ShortTermCapacity,
		Provider:          provider,
	}

	eng := engine.New(f, engCfg, tools, longMem)
	eng.SetRoles(roles)
	eng.Quiet()

	eng.SetProgress(func(ev engine.ProgressEvent) {
		switch ev.Kind {
		case "tick":
			emit(vizEvent{Type: "tick", Tick: ev.Tick, MaxTicks: ev.Total})
			// Send updated field snapshot each tick.
			fd := make(map[string]any)
			for _, pid := range regions {
				pt, ok := f.Point(pid)
				if ok {
					sigs := make(map[string]float64)
					for sig, val := range pt.Signals {
						if val > 0.01 {
							sigs[string(sig)] = val
						}
					}
					fd[pid] = sigs
				}
			}
			emit(vizEvent{Type: "field_update", Field: fd})

		case "spawn":
			emit(vizEvent{Type: "spawn", Agent: ev.Agent, Point: ev.Point, Tick: ev.Tick})

		case "differentiate":
			emit(vizEvent{Type: "differentiate", Agent: ev.Agent, Role: ev.Role, Point: ev.Point})

		case "work_start":
			emit(vizEvent{Type: "work_start", Total: ev.Total, Tick: ev.Tick})

		case "work_done":
			emit(vizEvent{Type: "work_done", Agent: ev.Agent, Role: ev.Role, Point: ev.Point,
				Done: ev.Alive, Total: ev.Total})

		case "apoptosis":
			emit(vizEvent{Type: "apoptosis", Agent: ev.Agent, Role: ev.Role, Point: ev.Point})

		case "complete":
			elapsed := time.Since(startTime).Round(time.Millisecond).String()
			emit(vizEvent{Type: "complete", Findings: ev.Finding, Total: ev.Total, Elapsed: elapsed})
		}
	})

	result := eng.Run()

	// Emit individual findings for the UI.
	for _, finding := range result.Findings {
		// Parse "[agent@point] role: content"
		parts := parseFinding(finding)
		emit(vizEvent{Type: "finding", Agent: parts[0], Point: parts[1], Role: parts[2], Detail: parts[3]})
	}

	// Emit tissues.
	seen := map[string]bool{}
	for _, t := range result.Tissues {
		if !seen[t] {
			emit(vizEvent{Type: "tissue", Detail: t})
			seen[t] = true
		}
	}

	elapsed := time.Since(startTime).Round(time.Millisecond).String()
	emit(vizEvent{Type: "complete", Findings: len(result.Findings), Total: result.AgentsTotal,
		Elapsed: elapsed, ByRole: result.ByRole, ByPoint: result.ByPoint})
}

func parseFinding(s string) [4]string {
	// Format: "[agent@point] role: content"
	result := [4]string{"?", "?", "?", s}
	if len(s) < 3 || s[0] != '[' {
		return result
	}
	close := 0
	for i, c := range s {
		if c == ']' {
			close = i
			break
		}
	}
	if close == 0 {
		return result
	}
	inner := s[1:close] // "agent@point"
	rest := s[close+2:] // "role: content"
	for i, c := range inner {
		if c == '@' {
			result[0] = inner[:i]
			result[1] = inner[i+1:]
			break
		}
	}
	for i, c := range rest {
		if c == ':' {
			result[2] = rest[:i]
			if i+2 < len(rest) {
				result[3] = rest[i+2:]
			}
			break
		}
	}
	return result
}

func serveSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := hub.add()
	defer hub.remove(ch)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case data := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

func emit(ev vizEvent) {
	data, _ := json.Marshal(ev)
	hub.broadcast(data)
}

func serveUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, dashboardHTML)
}

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Morpho — Morphogenetic Code Analysis</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { background: #0a0a0f; color: #c8c8d0; font-family: 'SF Mono', 'Fira Code', monospace; font-size: 13px; }

  .header { padding: 20px 24px; border-bottom: 1px solid #1a1a2e; display: flex; align-items: center; gap: 16px; }
  .header h1 { color: #00d4ff; font-size: 18px; letter-spacing: 2px; }
  .header .sub { color: #666; font-size: 12px; }
  .start-btn { background: #00d4ff; color: #0a0a0f; border: none; padding: 8px 20px; border-radius: 4px; cursor: pointer; font-family: inherit; font-weight: 600; font-size: 12px; letter-spacing: 1px; }
  .start-btn:hover { background: #00b8e0; }
  .start-btn:disabled { background: #333; color: #666; cursor: default; }

  .grid { display: grid; grid-template-columns: 1fr 1fr; grid-template-rows: auto 1fr; gap: 1px; background: #1a1a2e; height: calc(100vh - 65px); }
  .panel { background: #0d0d14; padding: 16px; overflow-y: auto; }
  .panel-title { color: #888; font-size: 10px; letter-spacing: 2px; text-transform: uppercase; margin-bottom: 12px; }

  /* Field visualization */
  .field-grid { display: flex; flex-wrap: wrap; gap: 8px; }
  .region { background: #111122; border: 1px solid #222; border-radius: 6px; padding: 10px 12px; min-width: 120px; position: relative; transition: all 0.3s; }
  .region.active { border-color: #00d4ff33; box-shadow: 0 0 12px #00d4ff11; }
  .region-name { font-weight: 600; color: #e0e0e8; margin-bottom: 6px; font-size: 12px; }
  .region-signals { display: flex; flex-direction: column; gap: 2px; }
  .signal-bar { display: flex; align-items: center; gap: 6px; font-size: 10px; }
  .signal-bar .label { width: 65px; color: #888; }
  .signal-bar .bar { flex: 1; height: 4px; background: #1a1a2e; border-radius: 2px; overflow: hidden; }
  .signal-bar .fill { height: 100%; border-radius: 2px; transition: width 0.5s; }
  .signal-bar .val { width: 30px; text-align: right; font-size: 9px; color: #666; }

  .agents-on-region { display: flex; gap: 3px; margin-top: 6px; flex-wrap: wrap; }
  .agent-dot { width: 8px; height: 8px; border-radius: 50%; transition: all 0.3s; }
  .agent-dot.working { animation: pulse 1s infinite; }
  @keyframes pulse { 0%,100% { opacity: 1; } 50% { opacity: 0.4; } }

  /* Agent log */
  .log-entry { padding: 4px 0; border-bottom: 1px solid #111; display: flex; gap: 8px; align-items: flex-start; font-size: 12px; }
  .log-icon { width: 16px; text-align: center; flex-shrink: 0; }
  .log-text { flex: 1; line-height: 1.4; }
  .log-time { color: #444; font-size: 10px; flex-shrink: 0; }

  /* Stats */
  .stats-row { display: flex; gap: 16px; flex-wrap: wrap; margin-bottom: 12px; }
  .stat { background: #111122; border-radius: 6px; padding: 10px 14px; min-width: 100px; }
  .stat-value { font-size: 22px; font-weight: 700; color: #00d4ff; }
  .stat-label { font-size: 10px; color: #666; margin-top: 2px; letter-spacing: 1px; }

  /* Progress bar */
  .progress { height: 3px; background: #1a1a2e; border-radius: 2px; margin: 12px 0; }
  .progress-fill { height: 100%; background: linear-gradient(90deg, #00d4ff, #7b68ee); border-radius: 2px; transition: width 0.5s; }

  /* Findings */
  .finding { background: #111122; border-radius: 6px; padding: 10px 12px; margin-bottom: 6px; font-size: 11px; line-height: 1.5; border-left: 3px solid #333; }
  .finding .role-tag { display: inline-block; padding: 1px 6px; border-radius: 3px; font-size: 9px; font-weight: 600; margin-right: 6px; letter-spacing: 0.5px; }

  /* Tissue clusters */
  .tissue { background: #111122; border-radius: 6px; padding: 8px 12px; margin-bottom: 4px; font-size: 11px; border-left: 3px solid #7b68ee; }

  /* Role colors */
  .role-bug_hunter { color: #ff6b6b; }
  .role-test_writer { color: #51cf66; }
  .role-security_auditor { color: #ffd43b; }
  .role-refactorer { color: #00d4ff; }
  .role-documenter { color: #748ffc; }
  .role-optimizer { color: #cc5de8; }

  .dot-bug_hunter { background: #ff6b6b; }
  .dot-test_writer { background: #51cf66; }
  .dot-security_auditor { background: #ffd43b; }
  .dot-refactorer { background: #00d4ff; }
  .dot-documenter { background: #748ffc; }
  .dot-optimizer { background: #cc5de8; }
  .dot-undifferentiated { background: #555; }

  .tag-bug_hunter { background: #ff6b6b22; color: #ff6b6b; border: 1px solid #ff6b6b44; }
  .tag-test_writer { background: #51cf6622; color: #51cf66; border: 1px solid #51cf6644; }
  .tag-security_auditor { background: #ffd43b22; color: #ffd43b; border: 1px solid #ffd43b44; }
  .tag-refactorer { background: #00d4ff22; color: #00d4ff; border: 1px solid #00d4ff44; }
  .tag-documenter { background: #748ffc22; color: #748ffc; border: 1px solid #748ffc44; }
  .tag-optimizer { background: #cc5de822; color: #cc5de8; border: 1px solid #cc5de844; }

  .bottom-panel { grid-column: 1 / -1; }
  .findings-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
</style>
</head>
<body>

<div class="header">
  <h1>MORPHO</h1>
  <span class="sub">morphogenetic code analysis</span>
  <div style="flex:1"></div>
  <span class="sub" id="status">Ready</span>
  <button class="start-btn" id="startBtn" onclick="startRun()">START</button>
</div>

<div class="grid">
  <!-- Top-left: Gradient Field -->
  <div class="panel">
    <div class="panel-title">Gradient Field</div>
    <div class="progress"><div class="progress-fill" id="progressBar" style="width:0%"></div></div>
    <div class="field-grid" id="fieldGrid"></div>
  </div>

  <!-- Top-right: Agent Activity -->
  <div class="panel">
    <div class="panel-title">Agent Activity</div>
    <div class="stats-row" id="statsRow">
      <div class="stat"><div class="stat-value" id="statFindings">0</div><div class="stat-label">Findings</div></div>
      <div class="stat"><div class="stat-value" id="statAgents">0</div><div class="stat-label">Alive</div></div>
      <div class="stat"><div class="stat-value" id="statTick">0</div><div class="stat-label">Tick</div></div>
      <div class="stat"><div class="stat-value" id="statTime">0s</div><div class="stat-label">Elapsed</div></div>
    </div>
    <div id="activityLog" style="max-height: 400px; overflow-y: auto;"></div>
  </div>

  <!-- Bottom: Findings -->
  <div class="panel bottom-panel">
    <div class="panel-title">Findings & Tissues</div>
    <div id="tissueSection" style="margin-bottom: 12px;"></div>
    <div class="findings-grid" id="findingsGrid"></div>
  </div>
</div>

<script>
const state = {
  regions: {},       // name -> { signals, agents: [] }
  agents: {},        // id -> { role, point, status }
  findings: [],
  tissues: [],
  tick: 0,
  maxTicks: 0,
  totalFindings: 0,
  alive: 0,
  startTime: null,
};

const signalColors = {
  complexity: '#00d4ff',
  bug_density: '#ff6b6b',
  test_coverage: '#51cf66',
  security: '#ffd43b',
  performance: '#cc5de8',
  doc_debt: '#748ffc',
};

function startRun() {
  document.getElementById('startBtn').disabled = true;
  document.getElementById('status').textContent = 'Running...';
  state.startTime = Date.now();

  // Start SSE listener
  const es = new EventSource('/events');
  es.onmessage = (e) => {
    const ev = JSON.parse(e.data);
    handleEvent(ev);
  };
  es.onerror = () => {
    document.getElementById('status').textContent = 'Connection lost';
  };

  // Trigger the run
  fetch('/run');

  // Elapsed timer
  setInterval(() => {
    if (state.startTime) {
      const s = Math.round((Date.now() - state.startTime) / 1000);
      document.getElementById('statTime').textContent = s + 's';
    }
  }, 1000);
}

function handleEvent(ev) {
  switch (ev.type) {
    case 'init':
      ev.regions.forEach(r => {
        state.regions[r] = { signals: {}, agents: [] };
      });
      if (ev.field) {
        for (const [region, signals] of Object.entries(ev.field)) {
          if (state.regions[region]) {
            state.regions[region].signals = signals;
          }
        }
      }
      renderField();
      break;

    case 'tick':
      state.tick = ev.tick;
      state.maxTicks = ev.maxTicks;
      document.getElementById('statTick').textContent = ev.tick + '/' + ev.maxTicks;
      document.getElementById('progressBar').style.width = ((ev.tick / ev.maxTicks) * 100) + '%';
      addLog('⏱', 'Tick ' + ev.tick + '/' + ev.maxTicks, '#888');
      break;

    case 'spawn':
      state.agents[ev.agent] = { role: 'undifferentiated', point: ev.point, status: 'idle' };
      if (state.regions[ev.point]) {
        state.regions[ev.point].agents.push(ev.agent);
      }
      state.alive++;
      document.getElementById('statAgents').textContent = state.alive;
      addLog('+', ev.agent + ' spawned at ' + ev.point, '#51cf66');
      renderField();
      break;

    case 'differentiate':
      if (state.agents[ev.agent]) {
        state.agents[ev.agent].role = ev.role;
      }
      addLog('~', ev.agent + ' → <span class="role-' + ev.role + '">' + ev.role + '</span> at ' + ev.point, '#ffd43b');
      renderField();
      break;

    case 'work_start':
      // Mark working agents
      for (const [id, a] of Object.entries(state.agents)) {
        if (a.status !== 'dead') a.status = 'working';
      }
      addLog('⚡', ev.total + ' agents analyzing (parallel)', '#00d4ff');
      renderField();
      break;

    case 'work_done':
      if (state.agents[ev.agent]) {
        state.agents[ev.agent].status = 'done';
      }
      state.totalFindings = ev.done; // reuse as counter
      addLog('✓', '<span class="role-' + ev.role + '">' + ev.role + '</span> ' + ev.agent + '@' + ev.point + ' [' + ev.done + '/' + ev.total + ']', '#51cf66');
      renderField();
      break;

    case 'apoptosis':
      if (state.agents[ev.agent]) {
        state.agents[ev.agent].status = 'dead';
      }
      state.alive = Math.max(0, state.alive - 1);
      document.getElementById('statAgents').textContent = state.alive;
      addLog('✗', ev.agent + ' died (' + ev.role + ') at ' + ev.point, '#ff6b6b');
      renderField();
      break;

    case 'field_update':
      if (ev.field) {
        for (const [region, signals] of Object.entries(ev.field)) {
          if (state.regions[region]) {
            state.regions[region].signals = signals;
          }
        }
      }
      renderField();
      break;

    case 'finding':
      state.findings.push({ role: ev.role, point: ev.point, agent: ev.agent, text: ev.detail });
      document.getElementById('statFindings').textContent = state.findings.length;
      renderFindings();
      break;

    case 'tissue':
      state.tissues.push(ev.detail);
      renderTissues();
      break;

    case 'complete':
      document.getElementById('status').textContent = 'Complete — ' + ev.findings + ' findings';
      document.getElementById('startBtn').disabled = false;
      document.getElementById('startBtn').textContent = 'RESTART';
      addLog('✔', 'Complete! ' + ev.findings + ' findings in ' + ev.elapsed, '#00d4ff');
      // Final field update
      if (ev.byRole) renderFinalStats(ev);
      break;
  }
}

function renderField() {
  const grid = document.getElementById('fieldGrid');
  grid.innerHTML = '';
  for (const [name, data] of Object.entries(state.regions)) {
    const div = document.createElement('div');
    div.className = 'region';

    // Check if any agent is active here
    const activeAgents = Object.entries(state.agents).filter(([id, a]) => a.point === name && a.status !== 'dead');
    if (activeAgents.length > 0) div.classList.add('active');

    let html = '<div class="region-name">' + name + '</div><div class="region-signals">';
    const signals = data.signals || {};
    for (const [sig, val] of Object.entries(signals)) {
      if (val < 0.01) continue;
      const pct = Math.min(val * 100, 100);
      const color = signalColors[sig] || '#888';
      html += '<div class="signal-bar"><span class="label">' + sig.replace('_', ' ') + '</span><div class="bar"><div class="fill" style="width:' + pct + '%;background:' + color + '"></div></div><span class="val">' + val.toFixed(2) + '</span></div>';
    }
    html += '</div>';

    // Agent dots
    if (activeAgents.length > 0) {
      html += '<div class="agents-on-region">';
      for (const [id, a] of activeAgents) {
        const cls = a.status === 'working' ? 'working' : '';
        html += '<div class="agent-dot dot-' + a.role + ' ' + cls + '" title="' + id + ' (' + a.role + ')"></div>';
      }
      html += '</div>';
    }

    div.innerHTML = html;
    grid.appendChild(div);
  }
}

function addLog(icon, text, color) {
  const log = document.getElementById('activityLog');
  const elapsed = state.startTime ? Math.round((Date.now() - state.startTime) / 1000) + 's' : '';
  log.innerHTML = '<div class="log-entry"><span class="log-icon" style="color:' + color + '">' + icon + '</span><span class="log-text">' + text + '</span><span class="log-time">' + elapsed + '</span></div>' + log.innerHTML;
  // Keep only last 200 entries
  while (log.children.length > 200) log.removeChild(log.lastChild);
}

function renderFindings() {
  const grid = document.getElementById('findingsGrid');
  // Show latest 20
  const latest = state.findings.slice(-20).reverse();
  grid.innerHTML = latest.map(f => {
    const text = f.text.length > 300 ? f.text.substring(0, 300) + '...' : f.text;
    return '<div class="finding"><span class="role-tag tag-' + f.role + '">' + f.role + '</span> <strong>' + f.agent + '@' + f.point + '</strong><br>' + escapeHtml(text) + '</div>';
  }).join('');
}

function renderTissues() {
  const section = document.getElementById('tissueSection');
  const unique = [...new Set(state.tissues)];
  section.innerHTML = unique.map(t => '<div class="tissue">🧬 ' + escapeHtml(t) + '</div>').join('');
}

function renderFinalStats(ev) {
  // Could expand this with charts etc.
}

function escapeHtml(s) {
  const d = document.createElement('div');
  d.textContent = s;
  return d.innerHTML;
}
</script>
</body>
</html>
`
