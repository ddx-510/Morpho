package tool

import (
	"os"
	"path/filepath"
	"strings"
)

// MarkdownSkill is a skill loaded from a .md file.
type MarkdownSkill struct {
	Name        string
	Description string
	Roles       []string // which roles this skill applies to
	Content     string   // the markdown body
}

// SkillLibrary holds all loaded markdown skills.
type SkillLibrary struct {
	skills []MarkdownSkill
}

// LoadSkillLibrary reads all .md files from a directory.
func LoadSkillLibrary(dir string) (*SkillLibrary, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	lib := &SkillLibrary{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		skill := parseSkillFile(string(data), e.Name())
		lib.skills = append(lib.skills, skill)
	}
	return lib, nil
}

// ForRole returns skills matching a given role name.
func (lib *SkillLibrary) ForRole(role string) []MarkdownSkill {
	if lib == nil {
		return nil
	}
	roleLower := strings.ToLower(role)
	var matched []MarkdownSkill
	for _, s := range lib.skills {
		if len(s.Roles) == 0 {
			// No roles specified = universal skill
			matched = append(matched, s)
			continue
		}
		for _, r := range s.Roles {
			if strings.ToLower(r) == roleLower || strings.Contains(roleLower, strings.ToLower(r)) {
				matched = append(matched, s)
				break
			}
		}
	}
	return matched
}

// AllSkills returns all loaded skills.
func (lib *SkillLibrary) AllSkills() []MarkdownSkill {
	if lib == nil {
		return nil
	}
	return lib.skills
}

// parseSkillFile extracts YAML frontmatter and markdown body from a skill file.
func parseSkillFile(content, filename string) MarkdownSkill {
	skill := MarkdownSkill{
		Name: strings.TrimSuffix(filename, ".md"),
	}

	// Parse YAML frontmatter between --- delimiters
	if strings.HasPrefix(content, "---\n") {
		end := strings.Index(content[4:], "\n---")
		if end >= 0 {
			frontmatter := content[4 : 4+end]
			content = strings.TrimSpace(content[4+end+4:])
			parseFrontmatter(&skill, frontmatter)
		}
	}

	skill.Content = content
	return skill
}

// parseFrontmatter extracts key-value pairs from simple YAML.
func parseFrontmatter(skill *MarkdownSkill, fm string) {
	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])

		switch key {
		case "name":
			skill.Name = val
		case "description":
			skill.Description = val
		case "roles":
			skill.Roles = parseYAMLList(val)
		}
	}
}

// parseYAMLList parses [a, b, c] or a simple comma-separated string.
func parseYAMLList(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "[]")
	var items []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		part = strings.Trim(part, "\"'")
		if part != "" {
			items = append(items, part)
		}
	}
	return items
}
