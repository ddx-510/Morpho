package domain

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/ddx-510/Morpho/agent"
	"github.com/ddx-510/Morpho/llm"
	"github.com/ddx-510/Morpho/tool"
)

// autoDomainSchema is what the LLM generates to define a domain.
type autoDomainSchema struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Signals     []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"signals"`
	Roles []struct {
		Name        string `json:"name"`
		Signal      string `json:"signal"`
		Description string `json:"description"`
		Prompt      string `json:"prompt"`
	} `json:"roles"`
	Regions []struct {
		Name    string             `json:"name"`
		Signals map[string]float64 `json:"signals"`
	} `json:"regions"`
}

// Auto uses an LLM to generate a domain definition from a free-text task.
func Auto(provider llm.Provider, task string, inputPath string) (*Domain, error) {
	stats, _ := dirStats(inputPath)
	hasFiles := len(stats) > 0

	systemPrompt := autoPrompt(hasFiles)
	userPrompt := fmt.Sprintf("Task: %s", task)
	if hasFiles {
		var summary strings.Builder
		for id, s := range stats {
			fmt.Fprintf(&summary, "  %s: %d files, %d lines\n", id, s.Files, s.Lines)
		}
		userPrompt += fmt.Sprintf("\n\nInput directory structure:\n%s", summary.String())
	}

	resp := provider.Generate(llm.Request{
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
	})
	if resp.Err != nil {
		return nil, fmt.Errorf("LLM domain generation failed: %w", resp.Err)
	}

	content := strings.TrimSpace(resp.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)
	content = repairJSON(content)

	var schema autoDomainSchema
	if err := json.Unmarshal([]byte(content), &schema); err != nil {
		return nil, fmt.Errorf("failed to parse LLM domain response: %w\nResponse was: %s", err, content[:min(len(content), 500)])
	}

	if len(schema.Signals) == 0 || len(schema.Roles) == 0 {
		return nil, fmt.Errorf("LLM generated empty domain (no signals or roles)")
	}

	d := &Domain{
		Name:        toSnakeCase(schema.Name),
		Description: schema.Description,
	}

	for _, s := range schema.Signals {
		d.Signals = append(d.Signals, SignalDef{
			Name:        agent.Signal(toSnakeCase(s.Name)),
			Description: s.Description,
		})
	}

	for _, r := range schema.Roles {
		d.Roles = append(d.Roles, RoleDef{
			Name:        toSnakeCase(r.Name),
			Signal:      agent.Signal(toSnakeCase(r.Signal)),
			Description: r.Description,
			Prompt:      r.Prompt,
		})
	}

	// Validate: each role's signal must match a defined signal.
	signalSet := make(map[agent.Signal]bool)
	for _, s := range d.Signals {
		signalSet[s.Name] = true
	}
	for i, r := range d.Roles {
		if !signalSet[r.Signal] && len(d.Signals) > 0 {
			d.Roles[i].Signal = d.Signals[i%len(d.Signals)].Name
		}
	}

	d.Seeder = func(input string) (*agent.GradientField, error) {
		if hasFiles {
			return seedFromStats(stats, d, input)
		}
		return seedFromRegions(schema.Regions, d)
	}
	d.ToolBuilder = func(input string) *tool.Registry {
		return tool.DefaultRegistry(input)
	}

	return d, nil
}

func autoPrompt(hasFiles bool) string {
	regionInstructions := ""
	if !hasFiles {
		regionInstructions = `
- ALSO generate 3-6 "regions" — subtopics or aspects of the task.
  Each region has signal values (0.0-1.0) indicating how much each signal applies there.
  Format: "regions": [{"name": "snake_case", "signals": {"signal_name": 0.7, ...}}]`
	}

	return fmt.Sprintf(`You are a domain architect for a multi-agent analysis system.
Given a task, design the analysis dimensions (signals) and specialist roles.

CRITICAL RULES:
- All "name" fields MUST be snake_case identifiers (e.g. "data_quality", NOT "Data Quality")
- Signal names and role names: lowercase, underscores, no spaces
- Create exactly 4 signals and 4 roles (keep response short!)
- Each role's "signal" must match one of the signal names exactly
- Each role prompt must be 2-3 sentences max
- Use {{.Region}}, {{.Value}}, {{.Code}} in prompts%s
- Respond with ONLY valid JSON, no markdown

JSON format:
{"name":"domain_name","description":"short desc","signals":[{"name":"snake_case","description":"desc"}],"roles":[{"name":"snake_case","signal":"matching_signal","description":"desc","prompt":"You are a X specialist analyzing {{.Region}}. Signal={{.Value}}. Find Y issues in:\n{{.Code}}\nOutput numbered findings with severity."}]%s}`,
		regionInstructions,
		func() string {
			if !hasFiles {
				return `,"regions":[{"name":"subtopic","signals":{"signal_name":0.7}}]`
			}
			return ""
		}())
}

func seedFromStats(stats map[string]*regionStats, d *Domain, rootDir string) (*agent.GradientField, error) {
	f := agent.NewField()

	if len(stats) == 0 {
		sigs := make(map[agent.Signal]float64)
		for _, s := range d.Signals {
			sigs[s.Name] = 0.5
		}
		f.AddPoint(&agent.Point{ID: "root", Signals: sigs})
		return f, nil
	}

	for id, s := range stats {
		if s.Files == 0 {
			continue
		}

		base := 0.15
		fileWeight := math.Min(float64(s.Files)*0.04, 0.4)
		lineWeight := math.Min(float64(s.Lines)/2000.0, 0.4)
		base += fileWeight + lineWeight
		base = clamp(base)

		sigs := make(map[agent.Signal]float64)
		for _, sig := range d.Signals {
			h := signalHash(id, string(sig.Name))
			multiplier := 0.3 + float64(h%70)/100.0
			sigs[sig.Name] = clamp(base * multiplier)
		}

		// Pre-load region content into the field (stigmergic knowledge).
		content := preloadRegionContent(rootDir, id)

		f.AddPoint(&agent.Point{ID: id, Signals: sigs, Links: s.Links, Content: content})
	}

	return f, nil
}

// preloadRegionContent reads key files from a region and returns a content
// snapshot that agents will see directly — no tool calls needed.
// Budget: ~20K chars per region to keep LLM context manageable.
func preloadRegionContent(root, regionID string) string {
	dir := filepath.Join(root, regionID)
	if regionID == "." {
		dir = root
	}

	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return ""
	}

	var files []string
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			name := info.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" || name == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}
		// Only source files.
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".go", ".js", ".jsx", ".ts", ".tsx", ".py", ".rs", ".java", ".rb",
			".c", ".cpp", ".h", ".md", ".yaml", ".yml", ".toml", ".sh", ".sql",
			".json", ".css", ".html", ".vue", ".svelte":
			rel, _ := filepath.Rel(root, path)
			files = append(files, rel)
		}
		return nil
	})

	if len(files) == 0 {
		return "(no source files)"
	}

	// Build listing + read a sample of files.
	var buf strings.Builder
	fmt.Fprintf(&buf, "FILES IN REGION %q (%d files):\n", regionID, len(files))
	for _, f := range files {
		buf.WriteString("  " + f + "\n")
	}
	buf.WriteString("\n")

	// Read files until budget exhausted.
	const budget = 20000
	sampled := 0
	for _, rel := range files {
		if buf.Len() > budget {
			fmt.Fprintf(&buf, "\n... (%d more files not shown, budget reached)\n", len(files)-sampled)
			break
		}
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		content := string(data)
		if len(content) > 3000 {
			content = content[:3000] + "\n... (truncated)"
		}
		fmt.Fprintf(&buf, "=== %s ===\n%s\n\n", rel, content)
		sampled++
	}
	return buf.String()
}

func signalHash(region, signal string) uint64 {
	var h uint64 = 5381
	for _, c := range region + "|" + signal {
		h = h*33 + uint64(c)
	}
	return h
}

func seedFromRegions(regions []struct {
	Name    string             `json:"name"`
	Signals map[string]float64 `json:"signals"`
}, d *Domain) (*agent.GradientField, error) {
	f := agent.NewField()

	if len(regions) == 0 {
		sigs := make(map[agent.Signal]float64)
		for _, s := range d.Signals {
			sigs[s.Name] = 0.5
		}
		f.AddPoint(&agent.Point{ID: "root", Signals: sigs})
		return f, nil
	}

	ids := make([]string, 0, len(regions))
	for _, r := range regions {
		ids = append(ids, toSnakeCase(r.Name))
	}

	for i, r := range regions {
		id := toSnakeCase(r.Name)
		sigs := make(map[agent.Signal]float64)

		for sigName, val := range r.Signals {
			sigs[agent.Signal(toSnakeCase(sigName))] = clamp(val)
		}

		for _, s := range d.Signals {
			if _, ok := sigs[s.Name]; !ok {
				sigs[s.Name] = 0.2
			}
		}

		var links []string
		for j, other := range ids {
			if j != i {
				links = append(links, other)
			}
		}

		f.AddPoint(&agent.Point{ID: id, Signals: sigs, Links: links})
	}

	return f, nil
}

func toSnakeCase(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	var result strings.Builder
	for i, r := range s {
		if r == ' ' || r == '-' {
			result.WriteRune('_')
		} else if r >= 'A' && r <= 'Z' {
			if i > 0 {
				prev := rune(s[i-1])
				if prev != '_' && prev != ' ' && prev != '-' && !(prev >= 'A' && prev <= 'Z') {
					result.WriteRune('_')
				}
			}
			result.WriteRune(r + 32)
		} else {
			result.WriteRune(r)
		}
	}
	out := result.String()
	for strings.Contains(out, "__") {
		out = strings.ReplaceAll(out, "__", "_")
	}
	return strings.Trim(out, "_")
}

func repairJSON(s string) string {
	if json.Valid([]byte(s)) {
		return s
	}

	inString := false
	escaped := false
	var stack []byte

	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && inString {
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch c {
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if len(stack) > 0 && stack[len(stack)-1] == c {
				stack = stack[:len(stack)-1]
			}
		}
	}

	if len(stack) == 0 && !inString {
		return s
	}

	repaired := s
	if inString {
		repaired += `"`
	}

	repaired = strings.TrimRight(repaired, " \t\n\r,:")

	for i := len(stack) - 1; i >= 0; i-- {
		repaired += string(stack[i])
	}

	if json.Valid([]byte(repaired)) {
		return repaired
	}
	return s
}

func clamp(v float64) float64 {
	return math.Max(0, math.Min(1, v))
}
