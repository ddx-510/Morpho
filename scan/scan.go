package scan

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ddx-510/Morpho/field"
)

// Dir scans a directory and returns a seeded gradient field.
// Each subdirectory (or the root if flat) becomes a point.
// Signals are derived from file heuristics — no LLM calls needed.
func Dir(root string) (*field.GradientField, error) {
	f := field.New()
	groups := map[string]*stats{}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		name := info.Name()
		if info.IsDir() {
			if name == ".git" || name == "node_modules" || name == "vendor" || name == "__pycache__" || name == "~" {
				return filepath.SkipDir
			}
			// Skip hidden directories (but not root).
			if strings.HasPrefix(name, ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}

		rel, _ := filepath.Rel(root, path)
		group := groupName(rel)
		s, ok := groups[group]
		if !ok {
			s = &stats{}
			groups[group] = s
		}

		s.files++

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		content := string(data)
		lines := strings.Count(content, "\n") + 1
		s.lines += lines

		lower := strings.ToLower(content)

		// Test coverage: presence of test files.
		if strings.Contains(name, "_test.") || strings.Contains(name, ".test.") || strings.Contains(name, ".spec.") {
			s.testFiles++
		}

		// Bug signals: TODO, FIXME, HACK, BUG, XXX.
		s.todos += strings.Count(lower, "todo")
		s.todos += strings.Count(lower, "fixme")
		s.todos += strings.Count(lower, "hack")
		s.todos += strings.Count(lower, "bug:")
		s.todos += strings.Count(lower, "xxx")

		// Security signals: hardcoded secrets, dangerous patterns.
		for _, pat := range []string{
			"password", "secret", "api_key", "apikey", "token",
			"exec(", "eval(", "dangerouslysetinnerhtml",
			"sql.query", "raw(", "nosec", "nolint",
			"http://", "chmod 777",
		} {
			s.secHits += strings.Count(lower, pat)
		}

		// Doc debt: ratio of comment lines.
		for _, line := range strings.Split(content, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") {
				s.commentLines++
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(groups) == 0 {
		return f, nil
	}

	// Build points from stats.
	pointIDs := make([]string, 0, len(groups))
	for id := range groups {
		pointIDs = append(pointIDs, id)
	}

	for _, id := range pointIDs {
		s := groups[id]
		if s.files == 0 {
			continue
		}

		sigs := map[field.Signal]float64{}

		// Complexity: based on lines of code per file.
		avgLines := float64(s.lines) / float64(s.files)
		sigs[field.Complexity] = clamp(avgLines / 300.0)

		// Bug density: todos per 100 lines.
		if s.lines > 0 {
			sigs[field.BugDensity] = clamp(float64(s.todos) / (float64(s.lines) / 100.0))
		}

		// Test coverage: inverse of test file ratio.
		if s.testFiles == 0 {
			sigs[field.TestCoverage] = clamp(0.3 + float64(s.files)*0.05)
		}

		// Security: hits per file.
		if s.secHits > 0 {
			sigs[field.Security] = clamp(float64(s.secHits) / float64(s.files) * 0.3)
		}

		// Performance: large files suggest perf debt.
		if avgLines > 200 {
			sigs[field.Performance] = clamp((avgLines - 200) / 500.0)
		}

		// Doc debt: inverse of comment ratio.
		if s.lines > 0 {
			commentRatio := float64(s.commentLines) / float64(s.lines)
			if commentRatio < 0.1 {
				sigs[field.DocDebt] = clamp(0.5 - commentRatio*5)
			}
		}

		// Link to all other points (small codebases) or neighbors.
		var links []string
		for _, other := range pointIDs {
			if other != id {
				links = append(links, other)
			}
		}

		f.AddPoint(&field.Point{
			ID:      id,
			Signals: sigs,
			Links:   links,
		})
	}

	return f, nil
}

type stats struct {
	files        int
	lines        int
	testFiles    int
	todos        int
	secHits      int
	commentLines int
}

// groupName turns a relative path into a group (top-level dir or "root").
func groupName(rel string) string {
	parts := strings.SplitN(rel, string(os.PathSeparator), 2)
	if len(parts) == 1 {
		return "root"
	}
	return parts[0]
}

func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
