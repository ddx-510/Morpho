package domain

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// regionStats holds raw stats for a scanned region.
type regionStats struct {
	Files int
	Lines int
	Links []string
}

// maxRegionFiles — regions larger than this get split into subdirectories.
const maxRegionFiles = 80

// maxRegions caps total regions to keep the swarm manageable.
const maxRegions = 20

// dirStats scans a directory and returns stats per region.
// Prefers `git ls-files` to respect .gitignore; falls back to filepath.Walk.
func dirStats(root string) (map[string]*regionStats, error) {
	allFiles, err := gitTrackedFiles(root)
	if err != nil {
		// Not a git repo or git not available — fall back to walk.
		allFiles, err = walkFiles(root)
		if err != nil {
			return nil, err
		}
	}

	if len(allFiles) == 0 {
		return map[string]*regionStats{}, nil
	}

	// Build regions by adaptive splitting.
	groups := buildRegions(allFiles)

	// Cap total regions — merge into nearest parent, not "other".
	if len(groups) > maxRegions {
		groups = mergeSmallest(groups, maxRegions)
	}

	// Build semantic links: scan imports to find cross-directory dependencies,
	// then fall back to connecting all regions as neighbors.
	importLinks := scanImportLinks(root, allFiles, groups)

	ids := make([]string, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	for _, id := range ids {
		linkSet := map[string]bool{}
		// Add import-based links first (semantic topology).
		for _, linked := range importLinks[id] {
			if linked != id {
				linkSet[linked] = true
			}
		}
		// Add structural neighbors (all other regions) as fallback.
		for _, other := range ids {
			if other != id {
				linkSet[other] = true
			}
		}
		var links []string
		for l := range linkSet {
			links = append(links, l)
		}
		groups[id].Links = links
	}

	return groups, nil
}

type fileEntry struct {
	rel   string
	lines int
}

// gitTrackedFiles uses `git ls-files` to get only tracked (non-ignored) files.
func gitTrackedFiles(root string) ([]fileEntry, error) {
	cmd := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var files []fileEntry
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Estimate lines from file size.
		info, err := os.Stat(filepath.Join(root, line))
		if err != nil || info.IsDir() {
			continue
		}
		files = append(files, fileEntry{
			rel:   line,
			lines: int(info.Size()/40) + 1,
		})
	}
	return files, nil
}

// walkFiles is the fallback scanner for non-git directories.
func walkFiles(root string) ([]fileEntry, error) {
	skipDirs := map[string]bool{
		".git": true, "node_modules": true, "vendor": true,
		"__pycache__": true, ".next": true, "dist": true, "build": true,
	}

	var allFiles []fileEntry
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if skipDirs[name] {
				return filepath.SkipDir
			}
			if strings.HasPrefix(name, ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		allFiles = append(allFiles, fileEntry{
			rel:   rel,
			lines: int(info.Size()/40) + 1,
		})
		return nil
	})
	return allFiles, err
}

// buildRegions groups files into regions, recursively splitting any region
// that exceeds maxRegionFiles.
func buildRegions(files []fileEntry) map[string]*regionStats {
	// Initial grouping: top-level directory.
	initial := groupByDepth(files, 0)

	result := map[string]*regionStats{}
	for region, regionFiles := range initial {
		splitInto(region, regionFiles, 1, result)
	}
	return result
}

// groupByDepth groups files by path component at the given depth.
// Depth 0 = top-level dir (or "." for root files).
func groupByDepth(files []fileEntry, depth int) map[string][]fileEntry {
	groups := map[string][]fileEntry{}
	for _, f := range files {
		parts := strings.Split(f.rel, string(os.PathSeparator))
		var key string
		if len(parts) <= depth+1 {
			// File is at or above this depth — group under parent.
			if depth == 0 {
				key = "."
			} else {
				key = strings.Join(parts[:depth], "/")
			}
		} else {
			key = strings.Join(parts[:depth+1], "/")
		}
		groups[key] = append(groups[key], f)
	}
	return groups
}

// splitInto recursively splits a region if it's too large.
func splitInto(region string, files []fileEntry, depth int, result map[string]*regionStats) {
	if len(files) <= maxRegionFiles || depth > 5 {
		// Small enough or max depth reached — finalize this region.
		stats := &regionStats{Files: len(files)}
		for _, f := range files {
			stats.Lines += f.lines
		}
		result[region] = stats
		return
	}

	// Split by next path level.
	subs := map[string][]fileEntry{}
	for _, f := range files {
		parts := strings.Split(f.rel, string(os.PathSeparator))
		var key string
		if len(parts) <= depth+1 {
			// File directly in this region directory.
			key = region
		} else {
			key = region + "/" + parts[depth]
		}
		subs[key] = append(subs[key], f)
	}

	// If splitting produced only one sub that equals the parent, we can't split further.
	if len(subs) == 1 {
		for subRegion, subFiles := range subs {
			if subRegion == region {
				// No split possible — finalize.
				stats := &regionStats{Files: len(files)}
				for _, f := range files {
					stats.Lines += f.lines
				}
				result[region] = stats
				return
			}
			// Single child directory (e.g. apps → apps/www) — recurse into it.
			splitInto(subRegion, subFiles, depth+1, result)
			return
		}
	}

	// Recurse into each sub-region.
	for subRegion, subFiles := range subs {
		splitInto(subRegion, subFiles, depth+1, result)
	}
}

// mergeSmallest reduces region count by folding smallest regions into their
// nearest parent directory that already exists, or the root ".".
// Never creates synthetic names like "other".
func mergeSmallest(groups map[string]*regionStats, target int) map[string]*regionStats {
	type entry struct {
		id    string
		stats *regionStats
	}
	var entries []entry
	for id, s := range groups {
		entries = append(entries, entry{id, s})
	}
	// Sort largest first so we keep the big ones.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].stats.Files > entries[j].stats.Files
	})

	// Keep the largest regions as-is.
	result := map[string]*regionStats{}
	var overflow []entry
	for i, e := range entries {
		if i < target {
			result[e.id] = e.stats
		} else {
			overflow = append(overflow, e)
		}
	}

	// Merge each overflow region into the closest existing parent.
	for _, e := range overflow {
		merged := false
		// Walk up the path to find a parent region that was kept.
		path := e.id
		for {
			idx := strings.LastIndex(path, "/")
			if idx < 0 {
				break
			}
			path = path[:idx]
			if parent, ok := result[path]; ok {
				parent.Files += e.stats.Files
				parent.Lines += e.stats.Lines
				merged = true
				break
			}
		}
		// If no parent found, merge into root.
		if !merged {
			if root, ok := result["."]; ok {
				root.Files += e.stats.Files
				root.Lines += e.stats.Lines
			} else {
				result["."] = &regionStats{
					Files: e.stats.Files,
					Lines: e.stats.Lines,
				}
			}
		}
	}

	return result
}

// ── Semantic topology: import-based links ───────────────────────

var reImport = regexp.MustCompile(
	`(?m)` +
		`(?:import\s+"([^"]+)")` + // Go: import "path"
		`|(?:from\s+['"]([^'"]+)['"])` + // Python/JS: from "x" / from 'x'
		`|(?:require\s*\(\s*['"]([^'"]+)['"]\s*\))`, // JS: require("x")
)

// scanImportLinks analyzes source files for import statements and maps
// which regions import from which other regions. Creates semantic links.
func scanImportLinks(root string, files []fileEntry, regions map[string]*regionStats) map[string][]string {
	// Build mapping: file path → region.
	fileToRegion := map[string]string{}
	regionIDs := make([]string, 0, len(regions))
	for id := range regions {
		regionIDs = append(regionIDs, id)
	}
	// Sort longest first so deeper paths match before parents.
	sort.Slice(regionIDs, func(i, j int) bool { return len(regionIDs[i]) > len(regionIDs[j]) })

	for _, f := range files {
		for _, rid := range regionIDs {
			if rid == "." || strings.HasPrefix(f.rel, rid+"/") || filepath.Dir(f.rel) == rid {
				fileToRegion[f.rel] = rid
				break
			}
		}
	}

	links := map[string]map[string]bool{}
	for _, rid := range regionIDs {
		links[rid] = map[string]bool{}
	}

	for _, f := range files {
		srcRegion, ok := fileToRegion[f.rel]
		if !ok {
			continue
		}
		ext := strings.ToLower(filepath.Ext(f.rel))
		switch ext {
		case ".go", ".py", ".js", ".jsx", ".ts", ".tsx", ".rs", ".java":
		default:
			continue
		}

		file, err := os.Open(filepath.Join(root, f.rel))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(file)
		lineCount := 0
		for sc.Scan() {
			lineCount++
			if lineCount > 50 {
				break
			}
			matches := reImport.FindAllStringSubmatch(sc.Text(), -1)
			for _, m := range matches {
				importPath := ""
				for _, sub := range m[1:] {
					if sub != "" {
						importPath = sub
						break
					}
				}
				if importPath == "" {
					continue
				}
				for _, targetRegion := range regionIDs {
					if targetRegion == srcRegion || targetRegion == "." {
						continue
					}
					if strings.Contains(importPath, targetRegion) ||
						strings.Contains(importPath, filepath.Base(targetRegion)) {
						links[srcRegion][targetRegion] = true
						links[targetRegion][srcRegion] = true
					}
				}
			}
		}
		file.Close()
	}

	result := map[string][]string{}
	for region, linked := range links {
		for l := range linked {
			result[region] = append(result[region], l)
		}
	}
	return result
}
