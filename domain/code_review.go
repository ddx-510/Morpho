package domain

import (
	"github.com/ddx-510/Morpho/field"
	"github.com/ddx-510/Morpho/scan"
	"github.com/ddx-510/Morpho/tool"
)

// CodeReview returns the built-in code review domain.
// This is what Morpho originally was — a code analysis system.
func CodeReview() *Domain {
	return &Domain{
		Name:        "code_review",
		Description: "Analyze source code for bugs, security issues, test gaps, complexity, documentation debt, and performance problems.",
		Signals: []SignalDef{
			{Name: field.Complexity, Description: "Code complexity — long functions, deep nesting, god objects"},
			{Name: field.BugDensity, Description: "Bug indicators — TODOs, FIXMEs, error-prone patterns"},
			{Name: field.TestCoverage, Description: "Missing test coverage"},
			{Name: field.Security, Description: "Security issues — hardcoded secrets, injection, unsafe operations"},
			{Name: field.Performance, Description: "Performance problems — unnecessary allocations, O(n^2) loops"},
			{Name: field.DocDebt, Description: "Documentation debt — missing or misleading docs"},
		},
		Roles: []RoleDef{
			{
				Name: "bug_hunter", Signal: field.BugDensity, Emoji: "B",
				Description: "Finds logic errors, edge cases, nil dereferences, race conditions",
				Prompt: `You are a bug hunter specialist analyzing the "{{.Region}}" region of a codebase.
Signal bug_density = {{.Value}} (0=fine, 1=critical).

SOURCE CODE:
{{.Code}}

INSTRUCTIONS:
- Find BUGS: logic errors, edge cases, nil dereferences, race conditions, off-by-one errors.
- Cite file names, function names, and quote problematic code.
- Output a numbered list of concrete findings with severity (LOW/MEDIUM/HIGH/CRITICAL).
- Do NOT narrate — analyze the code above directly.`,
			},
			{
				Name: "test_writer", Signal: field.TestCoverage, Emoji: "T",
				Description: "Identifies missing test coverage and untested edge cases",
				Prompt: `You are a test coverage specialist analyzing the "{{.Region}}" region of a codebase.
Signal test_coverage = {{.Value}} (0=fine, 1=critical).

SOURCE CODE:
{{.Code}}

INSTRUCTIONS:
- Find MISSING TESTS: untested functions, untested edge cases, missing error path tests.
- Cite specific functions that need tests and explain what scenarios are uncovered.
- Output a numbered list of concrete findings with severity.
- Do NOT narrate — analyze the code above directly.`,
			},
			{
				Name: "security_auditor", Signal: field.Security, Emoji: "S",
				Description: "Finds injection vulnerabilities, hardcoded secrets, unsafe operations",
				Prompt: `You are a security auditor specialist analyzing the "{{.Region}}" region of a codebase.
Signal security = {{.Value}} (0=fine, 1=critical).

SOURCE CODE:
{{.Code}}

INSTRUCTIONS:
- Find SECURITY ISSUES: injection, hardcoded secrets, path traversal, unsafe deserialization, missing auth.
- Cite file names, function names, and quote the vulnerable code.
- Output a numbered list of concrete findings with severity.
- Do NOT narrate — analyze the code above directly.`,
			},
			{
				Name: "refactorer", Signal: field.Complexity, Emoji: "R",
				Description: "Identifies overly complex code, duplication, and structural issues",
				Prompt: `You are a refactoring specialist analyzing the "{{.Region}}" region of a codebase.
Signal complexity = {{.Value}} (0=fine, 1=critical).

SOURCE CODE:
{{.Code}}

INSTRUCTIONS:
- Find COMPLEXITY ISSUES: functions too long, god objects, deep nesting, code duplication.
- Cite file names, function names, and quote the problematic structures.
- Output a numbered list of concrete findings with severity.
- Do NOT narrate — analyze the code above directly.`,
			},
			{
				Name: "documenter", Signal: field.DocDebt, Emoji: "D",
				Description: "Finds missing or misleading documentation",
				Prompt: `You are a documentation specialist analyzing the "{{.Region}}" region of a codebase.
Signal doc_debt = {{.Value}} (0=fine, 1=critical).

SOURCE CODE:
{{.Code}}

INSTRUCTIONS:
- Find DOCUMENTATION ISSUES: missing docs, misleading comments, unexported APIs without docs.
- Cite specific functions and types that need documentation.
- Output a numbered list of concrete findings with severity.
- Do NOT narrate — analyze the code above directly.`,
			},
			{
				Name: "optimizer", Signal: field.Performance, Emoji: "O",
				Description: "Finds performance issues and unnecessary allocations",
				Prompt: `You are a performance specialist analyzing the "{{.Region}}" region of a codebase.
Signal performance = {{.Value}} (0=fine, 1=critical).

SOURCE CODE:
{{.Code}}

INSTRUCTIONS:
- Find PERFORMANCE ISSUES: unnecessary allocations, O(n^2) loops, blocking calls, memory leaks.
- Cite file names, function names, and quote the slow code.
- Output a numbered list of concrete findings with severity.
- Do NOT narrate — analyze the code above directly.`,
			},
		},
		Seeder:      func(input string) (*field.GradientField, error) { return scan.Dir(input) },
		ToolBuilder: func(input string) *tool.Registry { return tool.DefaultRegistry(input) },
	}
}
