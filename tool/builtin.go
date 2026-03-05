package tool

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ReadFile reads a file from the workspace. If the path is a directory, lists its contents.
type ReadFile struct {
	WorkDir string
}

func (t *ReadFile) Name() string        { return "read_file" }
func (t *ReadFile) Description() string { return "Read a file's contents. If path is a directory, lists files in it." }
func (t *ReadFile) Parameters() map[string]string {
	return map[string]string{"path": "Relative path to the file (e.g. agent/agent.go)"}
}
func (t *ReadFile) Execute(args map[string]string) Result {
	raw := args["path"]
	if raw == "" {
		return Result{Err: fmt.Errorf("path is required")}
	}
	path := filepath.Join(t.WorkDir, filepath.Clean(raw))
	if !strings.HasPrefix(path, t.WorkDir) {
		return Result{Err: fmt.Errorf("path escapes workspace")}
	}

	info, err := os.Stat(path)
	if err != nil {
		return Result{Err: err}
	}

	// If it's a directory, list files instead of failing.
	if info.IsDir() {
		return listDir(path, t.WorkDir)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Result{Err: err}
	}
	content := string(data)
	if len(content) > 8000 {
		content = content[:8000] + "\n... (truncated)"
	}
	return Result{Output: content}
}

func listDir(dir, workDir string) Result {
	var files []string
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(workDir, path)
		files = append(files, rel)
		return nil
	})
	if len(files) == 0 {
		return Result{Output: "(empty directory)"}
	}
	return Result{Output: strings.Join(files, "\n")}
}

// GrepSearch searches file contents with a pattern.
type GrepSearch struct {
	WorkDir string
}

func (t *GrepSearch) Name() string        { return "grep" }
func (t *GrepSearch) Description() string { return "Search for a text pattern in files. Returns matching lines with file:line: prefix." }
func (t *GrepSearch) Parameters() map[string]string {
	return map[string]string{
		"pattern": "Text or regex pattern to search for",
		"path":    "Directory or file to search in (e.g. agent/ or agent/agent.go). Defaults to entire workspace.",
	}
}
func (t *GrepSearch) Execute(args map[string]string) Result {
	pattern := args["pattern"]
	if pattern == "" {
		return Result{Err: fmt.Errorf("pattern is required")}
	}
	searchPath := "."
	if p := args["path"]; p != "" {
		searchPath = p
	}
	cmdArgs := []string{"-rn", "--include=*.go", "--include=*.js", "--include=*.py", "--include=*.ts", "--include=*.rs", "--include=*.java", pattern, searchPath}
	cmd := exec.Command("grep", cmdArgs...)
	cmd.Dir = t.WorkDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		if len(out) == 0 {
			return Result{Output: "(no matches)"}
		}
	}
	output := string(out)
	if len(output) > 4000 {
		output = output[:4000] + "\n... (truncated)"
	}
	return Result{Output: output}
}

// PatchFile applies a simple find-and-replace patch to a file.
type PatchFile struct {
	WorkDir string
}

func (t *PatchFile) Name() string        { return "patch_file" }
func (t *PatchFile) Description() string { return "Replace text in a file (find and replace)" }
func (t *PatchFile) Parameters() map[string]string {
	return map[string]string{
		"path":    "Relative path to the file",
		"find":    "Text to find",
		"replace": "Text to replace it with",
	}
}
func (t *PatchFile) Execute(args map[string]string) Result {
	path := filepath.Join(t.WorkDir, filepath.Clean(args["path"]))
	if !strings.HasPrefix(path, t.WorkDir) {
		return Result{Err: fmt.Errorf("path escapes workspace")}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{Err: err}
	}
	content := string(data)
	if !strings.Contains(content, args["find"]) {
		return Result{Err: fmt.Errorf("find string not found in file")}
	}
	content = strings.Replace(content, args["find"], args["replace"], 1)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return Result{Err: err}
	}
	return Result{Output: fmt.Sprintf("patched %s", args["path"])}
}

// Shell runs a shell command in the workspace.
type Shell struct {
	WorkDir string
}

func (t *Shell) Name() string        { return "shell" }
func (t *Shell) Description() string { return "Run a shell command in the workspace (e.g. tests, builds)" }
func (t *Shell) Parameters() map[string]string {
	return map[string]string{"command": "Shell command to execute"}
}
func (t *Shell) Execute(args map[string]string) Result {
	cmd := exec.Command("sh", "-c", args["command"])
	cmd.Dir = t.WorkDir
	out, err := cmd.CombinedOutput()
	result := Result{Output: string(out)}
	if err != nil {
		result.Err = fmt.Errorf("%w: %s", err, string(out))
	}
	if len(result.Output) > 4000 {
		result.Output = result.Output[:4000] + "\n... (truncated)"
	}
	return result
}

// ListFiles lists all source files in a directory recursively.
type ListFiles struct {
	WorkDir string
}

func (t *ListFiles) Name() string { return "list_files" }
func (t *ListFiles) Description() string {
	return "List all source files in a directory recursively (e.g. path='agent' lists agent/*.go)"
}
func (t *ListFiles) Parameters() map[string]string {
	return map[string]string{"path": "Directory to list (e.g. 'agent', 'cmd/morpho'). Defaults to workspace root."}
}
func (t *ListFiles) Execute(args map[string]string) Result {
	dir := t.WorkDir
	if p := args["path"]; p != "" {
		dir = filepath.Join(t.WorkDir, filepath.Clean(p))
	}
	if !strings.HasPrefix(dir, t.WorkDir) {
		return Result{Err: fmt.Errorf("path escapes workspace")}
	}
	return listDir(dir, t.WorkDir)
}

// DefaultRegistry creates a registry with all built-in tools for a workspace.
func DefaultRegistry(workDir string) *Registry {
	r := NewRegistry()
	r.Register(&ReadFile{WorkDir: workDir})
	r.Register(&GrepSearch{WorkDir: workDir})
	r.Register(&PatchFile{WorkDir: workDir})
	r.Register(&Shell{WorkDir: workDir})
	r.Register(&ListFiles{WorkDir: workDir})
	return r
}
