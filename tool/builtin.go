package tool

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ddx-510/Morpho/llm"
)

// resolvePath resolves a path argument to an absolute path.
// Absolute paths are used directly. Relative paths resolve from workDir.
// ~ is expanded to the user's home directory.
func resolvePath(raw, workDir string) string {
	if raw == "" {
		return workDir
	}
	// Expand ~
	if strings.HasPrefix(raw, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			raw = filepath.Join(home, raw[2:])
		}
	}
	// Absolute paths pass through.
	if filepath.IsAbs(raw) {
		return filepath.Clean(raw)
	}
	// Relative paths resolve from workDir.
	return filepath.Join(workDir, filepath.Clean(raw))
}

// ── read_file ───────────────────────────────────────────────────────

// ReadFile reads a file with optional offset/limit and line numbers.
// Supports absolute paths, relative paths (from WorkDir), and ~ expansion.
type ReadFile struct {
	WorkDir string
}

func (t *ReadFile) Name() string { return "read_file" }
func (t *ReadFile) Description() string {
	return "Read a file's contents with line numbers. Supports offset and limit for large files. If path is a directory, lists its entries. Accepts absolute or relative paths."
}
func (t *ReadFile) Parameters() json.RawMessage {
	return llm.ParamSchema(map[string]llm.ParamDef{
		"path":   {Description: "Path to the file (absolute or relative to workspace)", Required: true},
		"offset": {Type: "integer", Description: "Line number to start reading from (1-based, default 1)"},
		"limit":  {Type: "integer", Description: "Maximum number of lines to return (default: entire file, max 500)"},
	})
}
func (t *ReadFile) Execute(args map[string]any) Result {
	raw := StringArg(args, "path")
	if raw == "" {
		return Result{Err: fmt.Errorf("path is required")}
	}
	path := resolvePath(raw, t.WorkDir)

	info, err := os.Stat(path)
	if err != nil {
		return Result{Err: err}
	}
	if info.IsDir() {
		return listDir(path, t.WorkDir)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Result{Err: err}
	}

	lines := strings.Split(string(data), "\n")
	total := len(lines)

	offset := IntArg(args, "offset", 1)
	if offset < 1 {
		offset = 1
	}
	limit := IntArg(args, "limit", 0)
	if limit <= 0 || limit > 500 {
		limit = 500
	}

	start := offset - 1
	if start >= total {
		return Result{Output: fmt.Sprintf("(file has %d lines, offset %d is past end)", total, offset)}
	}
	end := start + limit
	if end > total {
		end = total
	}

	var buf strings.Builder
	fmt.Fprintf(&buf, "// %s (%d lines total)\n", raw, total)
	for i := start; i < end; i++ {
		fmt.Fprintf(&buf, "%4d │ %s\n", i+1, lines[i])
	}
	if end < total {
		fmt.Fprintf(&buf, "... (%d more lines, use offset=%d to continue)\n", total-end, end+1)
	}
	return Result{Output: buf.String()}
}

// ── list_dir helper ─────────────────────────────────────────────────

// listDir lists files recursively, returning paths relative to relBase.
// This ensures agents always see workspace-relative paths like "domain/auto.go"
// rather than directory-relative paths like "auto.go".
func listDir(dir string, relBase ...string) Result {
	base := dir
	if len(relBase) > 0 && relBase[0] != "" {
		base = relBase[0]
	}
	const maxEntries = 200
	var entries []string
	count := 0
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" || name == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}
		count++
		if count <= maxEntries {
			rel, err := filepath.Rel(base, path)
			if err != nil {
				rel = path
			}
			entries = append(entries, rel)
		}
		return nil
	})
	if len(entries) == 0 {
		return Result{Output: "(empty directory)"}
	}
	result := strings.Join(entries, "\n")
	if count > maxEntries {
		result += fmt.Sprintf("\n... (%d more files not shown, %d total)", count-maxEntries, count)
	}
	return Result{Output: result}
}

// ── edit_file ───────────────────────────────────────────────────────

// EditFile applies a find-and-replace edit with unique-match enforcement.
type EditFile struct {
	WorkDir string
}

func (t *EditFile) Name() string { return "edit_file" }
func (t *EditFile) Description() string {
	return "Edit a file by replacing an exact text match. The old_string must appear exactly once in the file (unique match). To create a new file, set old_string to empty and provide new_string as the full content."
}
func (t *EditFile) Parameters() json.RawMessage {
	return llm.ParamSchema(map[string]llm.ParamDef{
		"path":       {Description: "Path to the file (absolute or relative to workspace)", Required: true},
		"old_string": {Description: "Exact text to find (must be unique in file). Empty string = create new file."},
		"new_string": {Description: "Replacement text", Required: true},
	})
}

func (t *EditFile) Execute(args map[string]any) Result {
	raw := StringArg(args, "path")
	if raw == "" {
		return Result{Err: fmt.Errorf("path is required")}
	}
	path := resolvePath(raw, t.WorkDir)

	oldStr := StringArg(args, "old_string")
	newStr := StringArg(args, "new_string")

	// Create new file mode.
	if oldStr == "" {
		dir := filepath.Dir(path)
		if dir != "." && dir != "" {
			os.MkdirAll(dir, 0755)
		}
		if err := os.WriteFile(path, []byte(newStr), 0644); err != nil {
			return Result{Err: err}
		}
		return Result{Output: fmt.Sprintf("created %s", raw)}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Result{Err: err}
	}
	content := string(data)

	count := strings.Count(content, oldStr)
	if count == 0 {
		hint := findBestMatch(content, oldStr)
		msg := fmt.Sprintf("old_string not found in %s", raw)
		if hint != "" {
			msg += fmt.Sprintf("\n\nDid you mean:\n%s", hint)
		}
		return Result{Err: fmt.Errorf("%s", msg)}
	}
	if count > 1 {
		return Result{Err: fmt.Errorf("old_string matches %d times in %s — must be unique. Add more surrounding context to disambiguate", count, raw)}
	}

	content = strings.Replace(content, oldStr, newStr, 1)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return Result{Err: err}
	}
	return Result{Output: fmt.Sprintf("edited %s", raw)}
}

// findBestMatch finds the most similar substring for error hints.
func findBestMatch(content, target string) string {
	lines := strings.Split(content, "\n")
	targetLines := strings.Split(target, "\n")
	if len(targetLines) == 0 {
		return ""
	}

	firstLine := strings.TrimSpace(targetLines[0])
	if firstLine == "" && len(targetLines) > 1 {
		firstLine = strings.TrimSpace(targetLines[1])
	}
	if firstLine == "" {
		return ""
	}

	bestIdx := -1
	bestScore := 0
	for i, line := range lines {
		score := similarity(strings.TrimSpace(line), firstLine)
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}

	if bestScore < len(firstLine)/3 {
		return ""
	}

	start := bestIdx
	end := bestIdx + len(targetLines) + 1
	if end > len(lines) {
		end = len(lines)
	}
	var buf strings.Builder
	for i := start; i < end; i++ {
		fmt.Fprintf(&buf, "%4d │ %s\n", i+1, lines[i])
	}
	return buf.String()
}

func similarity(a, b string) int {
	if len(a) > len(b) {
		a, b = b, a
	}
	score := 0
	for i := 0; i < len(a); i++ {
		if i < len(b) && a[i] == b[i] {
			score++
		}
	}
	return score
}

// ── grep ────────────────────────────────────────────────────────────

// GrepSearch searches file contents with a regex pattern.
type GrepSearch struct {
	WorkDir string
}

func (t *GrepSearch) Name() string { return "grep" }
func (t *GrepSearch) Description() string {
	return "Search for a literal text pattern in files. Returns matching lines with file:line prefix. Accepts absolute or relative paths. Use is_regex=true for regex patterns."
}
func (t *GrepSearch) Parameters() json.RawMessage {
	return llm.ParamSchema(map[string]llm.ParamDef{
		"pattern":  {Description: "Text pattern to search for (literal by default, regex if is_regex=true)", Required: true},
		"path":     {Description: "Directory or file to search in (absolute or relative). Defaults to workspace root."},
		"include":  {Description: "File glob pattern to filter (e.g. '*.go', '*.py'). Defaults to common source files."},
		"is_regex": {Type: "boolean", Description: "If true, treat pattern as regex instead of literal text. Default: false."},
	})
}

func (t *GrepSearch) Execute(args map[string]any) Result {
	pattern := StringArg(args, "pattern")
	if pattern == "" {
		return Result{Err: fmt.Errorf("pattern is required")}
	}

	searchPath := StringArg(args, "path")
	searchDir := t.WorkDir
	if searchPath != "" {
		resolved := resolvePath(searchPath, t.WorkDir)
		if info, err := os.Stat(resolved); err == nil {
			if info.IsDir() {
				searchDir = resolved
				searchPath = "."
			} else {
				searchDir = filepath.Dir(resolved)
				searchPath = filepath.Base(resolved)
			}
		} else {
			searchPath = resolved
			searchDir = t.WorkDir
		}
	} else {
		searchPath = "."
	}

	// Default to fixed-string mode (-F) to avoid regex escaping issues.
	// LLMs frequently pass patterns like "fetch(" which break grep regex.
	isRegex := false
	if v, ok := args["is_regex"]; ok {
		if b, ok := v.(bool); ok {
			isRegex = b
		}
	}

	cmdArgs := []string{"-rn"}
	if !isRegex {
		cmdArgs = append(cmdArgs, "-F")
	}

	include := StringArg(args, "include")
	if include != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--include=%s", include))
	} else {
		for _, ext := range []string{"*.go", "*.js", "*.jsx", "*.ts", "*.tsx", "*.py", "*.rs", "*.java", "*.rb", "*.c", "*.cpp", "*.h", "*.md", "*.yaml", "*.yml", "*.json", "*.toml", "*.sh", "*.sql"} {
			cmdArgs = append(cmdArgs, fmt.Sprintf("--include=%s", ext))
		}
	}

	cmdArgs = append(cmdArgs, pattern, searchPath)
	cmd := exec.Command("grep", cmdArgs...)
	cmd.Dir = searchDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		if len(out) == 0 {
			return Result{Output: "(no matches)"}
		}
	}
	output := string(out)
	if len(output) > 8000 {
		output = output[:8000] + "\n... (truncated)"
	}
	return Result{Output: output}
}

// ── shell ───────────────────────────────────────────────────────────

// Shell runs a shell command with security restrictions.
type Shell struct {
	WorkDir string
}

// Dangerous command patterns that are blocked.
var shellDenyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\brm\s+(-[^\s]*)?-r`),      // rm -rf, rm -r
	regexp.MustCompile(`\brm\s+(-[^\s]*)?-f`),       // rm -f
	regexp.MustCompile(`\brmdir\b`),
	regexp.MustCompile(`\bmkfs\b`),
	regexp.MustCompile(`\bdd\s+if=`),
	regexp.MustCompile(`>\s*/dev/`),
	regexp.MustCompile(`\bchmod\s+777\b`),
	regexp.MustCompile(`\bcurl\b.*\|\s*(sh|bash)\b`), // curl | sh
	regexp.MustCompile(`\bwget\b.*\|\s*(sh|bash)\b`),
	regexp.MustCompile(`\b:(){ :\|:& };:`),           // fork bomb
	regexp.MustCompile(`\bgit\s+push\b.*--force\b`),
	regexp.MustCompile(`\bgit\s+reset\s+--hard\b`),
}

func (t *Shell) Name() string { return "shell" }
func (t *Shell) Description() string {
	return "Run a shell command. Runs in the workspace directory by default, or specify a working directory. Some destructive commands are blocked for safety."
}
func (t *Shell) Parameters() json.RawMessage {
	return llm.ParamSchema(map[string]llm.ParamDef{
		"command": {Description: "Shell command to execute", Required: true},
		"cwd":     {Description: "Working directory for the command (absolute or relative). Defaults to workspace."},
		"timeout": {Type: "integer", Description: "Timeout in seconds (default 30, max 120)"},
	})
}

func (t *Shell) Execute(args map[string]any) Result {
	command := StringArg(args, "command")
	if command == "" {
		return Result{Err: fmt.Errorf("command is required")}
	}

	for _, pat := range shellDenyPatterns {
		if pat.MatchString(command) {
			return Result{Err: fmt.Errorf("blocked: command matches security deny pattern (%s)", pat.String())}
		}
	}

	cwd := t.WorkDir
	if d := StringArg(args, "cwd"); d != "" {
		cwd = resolvePath(d, t.WorkDir)
	}

	timeoutSec := IntArg(args, "timeout", 30)
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	if timeoutSec > 120 {
		timeoutSec = 120
	}

	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = cwd

	done := make(chan struct{})
	var out []byte
	var err error
	go func() {
		out, err = cmd.CombinedOutput()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Duration(timeoutSec) * time.Second):
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		return Result{Err: fmt.Errorf("command timed out after %ds", timeoutSec)}
	}

	result := Result{Output: string(out)}
	if err != nil {
		result.Err = fmt.Errorf("%w: %s", err, string(out))
	}
	if len(result.Output) > 8000 {
		result.Output = result.Output[:8000] + "\n... (truncated)"
	}
	return result
}

// ── list_files ──────────────────────────────────────────────────────

// ListFiles lists files in a directory recursively.
type ListFiles struct {
	WorkDir string
}

func (t *ListFiles) Name() string { return "list_files" }
func (t *ListFiles) Description() string {
	return "List files in a directory recursively. Accepts absolute or relative paths. Capped at 200 entries."
}
func (t *ListFiles) Parameters() json.RawMessage {
	return llm.ParamSchema(map[string]llm.ParamDef{
		"path": {Description: "Directory to list (absolute or relative). Defaults to workspace root."},
	})
}

func (t *ListFiles) Execute(args map[string]any) Result {
	raw := StringArg(args, "path")
	dir := resolvePath(raw, t.WorkDir)
	return listDir(dir, t.WorkDir)
}

// ── registry ────────────────────────────────────────────────────────

// DefaultRegistry creates a registry with all built-in tools for a workspace.
func DefaultRegistry(workDir string) *Registry {
	r := NewRegistry()
	r.Register(&ReadFile{WorkDir: workDir})
	r.Register(&EditFile{WorkDir: workDir})
	r.Register(&GrepSearch{WorkDir: workDir})
	r.Register(&Shell{WorkDir: workDir})
	r.Register(&ListFiles{WorkDir: workDir})
	return r
}
