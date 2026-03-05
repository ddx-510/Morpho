package tool

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ReadFile reads a file from the workspace.
type ReadFile struct {
	WorkDir string
}

func (t *ReadFile) Name() string        { return "read_file" }
func (t *ReadFile) Description() string { return "Read the contents of a file in the workspace" }
func (t *ReadFile) Parameters() map[string]string {
	return map[string]string{"path": "Relative path to the file"}
}
func (t *ReadFile) Execute(args map[string]string) Result {
	path := filepath.Join(t.WorkDir, filepath.Clean(args["path"]))
	// Prevent directory traversal outside workspace.
	if !strings.HasPrefix(path, t.WorkDir) {
		return Result{Err: fmt.Errorf("path escapes workspace")}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{Err: err}
	}
	return Result{Output: string(data)}
}

// GrepSearch searches file contents with a pattern.
type GrepSearch struct {
	WorkDir string
}

func (t *GrepSearch) Name() string        { return "grep" }
func (t *GrepSearch) Description() string { return "Search for a pattern in workspace files" }
func (t *GrepSearch) Parameters() map[string]string {
	return map[string]string{
		"pattern": "Text or regex pattern to search for",
		"glob":    "File glob to restrict search (e.g. *.go)",
	}
}
func (t *GrepSearch) Execute(args map[string]string) Result {
	pattern := args["pattern"]
	if pattern == "" {
		return Result{Err: fmt.Errorf("pattern is required")}
	}
	cmdArgs := []string{"-rn", "--include", args["glob"], pattern, "."}
	if args["glob"] == "" {
		cmdArgs = []string{"-rn", pattern, "."}
	}
	cmd := exec.Command("grep", cmdArgs...)
	cmd.Dir = t.WorkDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		if len(out) == 0 {
			return Result{Output: "(no matches)"}
		}
	}
	return Result{Output: string(out)}
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
	return result
}

// ListFiles lists files matching a glob in the workspace.
type ListFiles struct {
	WorkDir string
}

func (t *ListFiles) Name() string        { return "list_files" }
func (t *ListFiles) Description() string { return "List files matching a glob pattern in the workspace" }
func (t *ListFiles) Parameters() map[string]string {
	return map[string]string{"pattern": "Glob pattern (e.g. **/*.go, src/*.js)"}
}
func (t *ListFiles) Execute(args map[string]string) Result {
	pattern := args["pattern"]
	if pattern == "" {
		pattern = "*"
	}
	matches, err := filepath.Glob(filepath.Join(t.WorkDir, pattern))
	if err != nil {
		return Result{Err: err}
	}
	var rel []string
	for _, m := range matches {
		r, _ := filepath.Rel(t.WorkDir, m)
		rel = append(rel, r)
	}
	return Result{Output: strings.Join(rel, "\n")}
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
