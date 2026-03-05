package domain

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ddx-510/Morpho/field"
	"github.com/ddx-510/Morpho/scan"
	"github.com/ddx-510/Morpho/tool"
)

// Research returns a domain for research and analysis tasks.
// Input is a directory of documents (txt, md, pdf) or a topic description file.
// Agents differentiate into researchers, fact-checkers, synthesizers, etc.
func Research() *Domain {
	return &Domain{
		Name:        "research",
		Description: "Analyze documents and topics for research — find gaps, verify claims, synthesize insights, and identify contradictions.",
		Signals: []SignalDef{
			{Name: "claim_density", Description: "Density of unverified claims that need fact-checking"},
			{Name: "knowledge_gap", Description: "Missing information or unexplored angles"},
			{Name: "contradiction", Description: "Contradictory statements or conflicting sources"},
			{Name: "synthesis_need", Description: "Need for cross-document synthesis and connection-making"},
			{Name: "bias_risk", Description: "Potential bias, one-sided arguments, or missing perspectives"},
			{Name: "depth_deficit", Description: "Shallow analysis that needs deeper investigation"},
		},
		Roles: []RoleDef{
			{
				Name: "fact_checker", Signal: "claim_density", Emoji: "F",
				Description: "Verifies claims, identifies unsupported assertions",
				Prompt: `You are a fact-checker specialist analyzing the "{{.Region}}" section of a research corpus.
Signal claim_density = {{.Value}} (0=fine, 1=critical).

DOCUMENTS:
{{.Code}}

INSTRUCTIONS:
- Identify UNVERIFIED CLAIMS: statements presented as fact without evidence or citation.
- Flag logical fallacies, circular reasoning, and unsupported generalizations.
- Output a numbered list of specific claims that need verification with severity.`,
			},
			{
				Name: "gap_finder", Signal: "knowledge_gap", Emoji: "G",
				Description: "Identifies missing information and unexplored angles",
				Prompt: `You are a knowledge gap analyst examining the "{{.Region}}" section of a research corpus.
Signal knowledge_gap = {{.Value}} (0=fine, 1=critical).

DOCUMENTS:
{{.Code}}

INSTRUCTIONS:
- Find KNOWLEDGE GAPS: topics mentioned but not explored, missing context, unanswered questions.
- Identify areas where more research or data is needed.
- Output a numbered list of specific gaps with severity.`,
			},
			{
				Name: "contradiction_detector", Signal: "contradiction", Emoji: "X",
				Description: "Finds contradictory statements and conflicting information",
				Prompt: `You are a contradiction detector analyzing the "{{.Region}}" section of a research corpus.
Signal contradiction = {{.Value}} (0=fine, 1=critical).

DOCUMENTS:
{{.Code}}

INSTRUCTIONS:
- Find CONTRADICTIONS: statements that conflict with each other, inconsistent data, logical inconsistencies.
- Quote the conflicting passages and explain the contradiction.
- Output a numbered list of specific contradictions with severity.`,
			},
			{
				Name: "synthesizer", Signal: "synthesis_need", Emoji: "Y",
				Description: "Connects ideas across documents and creates insights",
				Prompt: `You are a synthesis specialist analyzing the "{{.Region}}" section of a research corpus.
Signal synthesis_need = {{.Value}} (0=fine, 1=critical).

DOCUMENTS:
{{.Code}}

INSTRUCTIONS:
- Find SYNTHESIS OPPORTUNITIES: connections between ideas, patterns across documents, emergent themes.
- Identify how different pieces of information relate to each other.
- Output a numbered list of specific insights and connections.`,
			},
			{
				Name: "bias_detector", Signal: "bias_risk", Emoji: "P",
				Description: "Identifies potential bias and missing perspectives",
				Prompt: `You are a bias detector analyzing the "{{.Region}}" section of a research corpus.
Signal bias_risk = {{.Value}} (0=fine, 1=critical).

DOCUMENTS:
{{.Code}}

INSTRUCTIONS:
- Find BIAS: one-sided arguments, missing counter-arguments, loaded language, cherry-picked data.
- Identify perspectives that are underrepresented or missing entirely.
- Output a numbered list of specific bias concerns with severity.`,
			},
			{
				Name: "deep_analyst", Signal: "depth_deficit", Emoji: "A",
				Description: "Provides deeper analysis where surface-level treatment is insufficient",
				Prompt: `You are a deep analysis specialist examining the "{{.Region}}" section of a research corpus.
Signal depth_deficit = {{.Value}} (0=fine, 1=critical).

DOCUMENTS:
{{.Code}}

INSTRUCTIONS:
- Find SHALLOW ANALYSIS: topics that need deeper investigation, oversimplified explanations, missing nuance.
- Explain what deeper analysis would reveal and why it matters.
- Output a numbered list of areas needing deeper treatment with severity.`,
			},
		},
		Seeder:      seedResearch,
		ToolBuilder: func(input string) *tool.Registry { return tool.DefaultRegistry(input) },
	}
}

// seedResearch creates a gradient field from a directory of documents.
func seedResearch(input string) (*field.GradientField, error) {
	f := field.New()

	info, err := os.Stat(input)
	if err != nil {
		return nil, err
	}

	type docGroup struct {
		files   int
		words   int
		claims  int // sentences with assertion patterns
		questions int
	}
	groups := map[string]*docGroup{}

	walkFn := func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(fi.Name()))
		if ext != ".txt" && ext != ".md" && ext != ".csv" && ext != ".json" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		content := string(data)
		lower := strings.ToLower(content)

		var groupName string
		if info.IsDir() {
			rel, _ := filepath.Rel(input, path)
			parts := strings.SplitN(rel, string(os.PathSeparator), 2)
			if len(parts) == 1 {
				groupName = "root"
			} else {
				groupName = parts[0]
			}
		} else {
			groupName = "root"
		}

		g, ok := groups[groupName]
		if !ok {
			g = &docGroup{}
			groups[groupName] = g
		}
		g.files++
		g.words += len(strings.Fields(content))

		// Heuristic signals
		for _, p := range []string{"always", "never", "clearly", "obviously", "everyone knows", "it is clear that", "studies show"} {
			g.claims += strings.Count(lower, p)
		}
		g.questions += strings.Count(content, "?")

		return nil
	}

	if info.IsDir() {
		filepath.Walk(input, walkFn)
	} else {
		walkFn(input, info, nil)
	}

	if len(groups) == 0 {
		// No documents found — create a single point from the input as a topic.
		f.AddPoint(&field.Point{
			ID: "topic",
			Signals: map[field.Signal]float64{
				"knowledge_gap":  0.8,
				"synthesis_need": 0.6,
				"depth_deficit":  0.7,
			},
		})
		return f, nil
	}

	pointIDs := make([]string, 0, len(groups))
	for id := range groups {
		pointIDs = append(pointIDs, id)
	}

	for _, id := range pointIDs {
		g := groups[id]
		sigs := map[field.Signal]float64{}

		// Claim density: assertions per 100 words
		if g.words > 0 {
			sigs["claim_density"] = clamp(float64(g.claims) / (float64(g.words) / 100.0))
		}
		// Knowledge gap: questions suggest gaps
		if g.words > 0 {
			sigs["knowledge_gap"] = clamp(float64(g.questions) / (float64(g.words) / 200.0))
		}
		// Synthesis need: more files = more synthesis needed
		sigs["synthesis_need"] = clamp(float64(g.files) * 0.15)
		// Depth deficit: short documents suggest shallow coverage
		avgWords := float64(g.words) / float64(max(g.files, 1))
		if avgWords < 500 {
			sigs["depth_deficit"] = clamp((500 - avgWords) / 500.0)
		}
		// Bias risk: high claim density suggests bias
		sigs["bias_risk"] = clamp(float64(g.claims) * 0.1)
		// Contradiction: multiple files = potential contradictions
		if g.files > 1 {
			sigs["contradiction"] = clamp(float64(g.files) * 0.1)
		}

		var links []string
		for _, other := range pointIDs {
			if other != id {
				links = append(links, other)
			}
		}

		f.AddPoint(&field.Point{ID: id, Signals: sigs, Links: links})
	}

	return f, nil
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

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// WritingReview returns a domain for reviewing written content.
func WritingReview() *Domain {
	return &Domain{
		Name:        "writing_review",
		Description: "Review written content for clarity, structure, tone, accuracy, and engagement.",
		Signals: []SignalDef{
			{Name: "clarity_debt", Description: "Unclear, ambiguous, or confusing passages"},
			{Name: "structure_issue", Description: "Poor organization, flow, or logical structure"},
			{Name: "tone_mismatch", Description: "Inconsistent tone, wrong register for audience"},
			{Name: "accuracy_risk", Description: "Factual claims that may be inaccurate"},
			{Name: "engagement_gap", Description: "Boring, dry, or unengaging sections"},
			{Name: "redundancy", Description: "Repetitive content, unnecessary filler"},
		},
		Roles: []RoleDef{
			{
				Name: "clarity_editor", Signal: "clarity_debt", Emoji: "C",
				Description: "Makes writing clearer and more precise",
				Prompt: `You are a clarity editor analyzing the "{{.Region}}" section of a document.
Signal clarity_debt = {{.Value}} (0=fine, 1=critical).

TEXT:
{{.Code}}

INSTRUCTIONS:
- Find CLARITY ISSUES: ambiguous sentences, jargon without explanation, confusing pronoun references, unclear antecedents.
- Quote the problematic passages and suggest clearer alternatives.
- Output a numbered list with severity.`,
			},
			{
				Name: "structure_reviewer", Signal: "structure_issue", Emoji: "S",
				Description: "Reviews document structure and logical flow",
				Prompt: `You are a structure reviewer analyzing the "{{.Region}}" section.
Signal structure_issue = {{.Value}} (0=fine, 1=critical).

TEXT:
{{.Code}}

INSTRUCTIONS:
- Find STRUCTURAL ISSUES: poor paragraph transitions, illogical ordering, missing topic sentences, buried conclusions.
- Output a numbered list with severity.`,
			},
			{
				Name: "tone_checker", Signal: "tone_mismatch", Emoji: "T",
				Description: "Checks for consistent and appropriate tone",
				Prompt: `You are a tone specialist analyzing the "{{.Region}}" section.
Signal tone_mismatch = {{.Value}} (0=fine, 1=critical).

TEXT:
{{.Code}}

INSTRUCTIONS:
- Find TONE ISSUES: shifts in formality, inappropriate humor, passive aggression, inconsistent voice.
- Quote examples and explain the mismatch.
- Output a numbered list with severity.`,
			},
			{
				Name: "fact_verifier", Signal: "accuracy_risk", Emoji: "V",
				Description: "Flags potentially inaccurate claims",
				Prompt: `You are a fact verification specialist analyzing "{{.Region}}".
Signal accuracy_risk = {{.Value}} (0=fine, 1=critical).

TEXT:
{{.Code}}

INSTRUCTIONS:
- Find ACCURACY CONCERNS: claims that may be wrong, outdated stats, misattributed quotes, questionable assertions.
- Output a numbered list with severity.`,
			},
			{
				Name: "engagement_editor", Signal: "engagement_gap", Emoji: "E",
				Description: "Makes content more engaging and compelling",
				Prompt: `You are an engagement editor analyzing "{{.Region}}".
Signal engagement_gap = {{.Value}} (0=fine, 1=critical).

TEXT:
{{.Code}}

INSTRUCTIONS:
- Find ENGAGEMENT ISSUES: dry passages, missed storytelling opportunities, walls of text, lack of examples.
- Suggest specific improvements.
- Output a numbered list with severity.`,
			},
			{
				Name: "redundancy_cutter", Signal: "redundancy", Emoji: "X",
				Description: "Identifies repetitive or redundant content",
				Prompt: `You are a redundancy specialist analyzing "{{.Region}}".
Signal redundancy = {{.Value}} (0=fine, 1=critical).

TEXT:
{{.Code}}

INSTRUCTIONS:
- Find REDUNDANCY: repeated points, unnecessary filler words, saying the same thing different ways.
- Quote the redundant passages.
- Output a numbered list with severity.`,
			},
		},
		Seeder:      seedResearch, // reuse document seeder
		ToolBuilder: func(input string) *tool.Registry { return tool.DefaultRegistry(input) },
	}
}

// DataAnalysis returns a domain for analyzing datasets and data pipelines.
func DataAnalysis() *Domain {
	return &Domain{
		Name:        "data_analysis",
		Description: "Analyze data files, schemas, and pipelines for quality, consistency, and insights.",
		Signals: []SignalDef{
			{Name: "data_quality", Description: "Data quality issues — nulls, inconsistencies, outliers"},
			{Name: "schema_drift", Description: "Schema inconsistencies or undocumented changes"},
			{Name: "pipeline_risk", Description: "Fragile data pipeline logic"},
			{Name: "insight_potential", Description: "Unexplored patterns or correlations in the data"},
			{Name: "privacy_risk", Description: "PII exposure, missing anonymization"},
			{Name: "documentation_gap", Description: "Missing data dictionary or pipeline docs"},
		},
		Roles: []RoleDef{
			{
				Name: "quality_auditor", Signal: "data_quality", Emoji: "Q",
				Description: "Finds data quality issues",
				Prompt: `You are a data quality auditor analyzing "{{.Region}}".
Signal data_quality = {{.Value}} (0=fine, 1=critical).

DATA/CODE:
{{.Code}}

Find data quality issues: nulls, type mismatches, outlier handling, validation gaps. Output numbered findings with severity.`,
			},
			{
				Name: "schema_reviewer", Signal: "schema_drift", Emoji: "S",
				Description: "Reviews schema consistency",
				Prompt: `You are a schema reviewer analyzing "{{.Region}}".
Signal schema_drift = {{.Value}}.

DATA/CODE:
{{.Code}}

Find schema issues: undocumented fields, type inconsistencies, missing constraints, naming conventions. Output numbered findings with severity.`,
			},
			{
				Name: "pipeline_analyst", Signal: "pipeline_risk", Emoji: "P",
				Description: "Analyzes data pipeline reliability",
				Prompt: `You are a pipeline analyst examining "{{.Region}}".
Signal pipeline_risk = {{.Value}}.

CODE:
{{.Code}}

Find pipeline risks: missing error handling, no idempotency, race conditions, missing retries, silent failures. Output numbered findings.`,
			},
			{
				Name: "insight_miner", Signal: "insight_potential", Emoji: "I",
				Description: "Discovers unexplored patterns",
				Prompt: `You are an insight mining specialist analyzing "{{.Region}}".
Signal insight_potential = {{.Value}}.

DATA/CODE:
{{.Code}}

Find unexplored patterns, correlations, and analytical opportunities. Output numbered insights.`,
			},
			{
				Name: "privacy_auditor", Signal: "privacy_risk", Emoji: "V",
				Description: "Finds PII and privacy concerns",
				Prompt: `You are a privacy auditor analyzing "{{.Region}}".
Signal privacy_risk = {{.Value}}.

DATA/CODE:
{{.Code}}

Find privacy issues: exposed PII, missing anonymization, GDPR concerns, logging of sensitive data. Output numbered findings with severity.`,
			},
			{
				Name: "data_documenter", Signal: "documentation_gap", Emoji: "D",
				Description: "Identifies missing data documentation",
				Prompt: `You are a data documentation specialist analyzing "{{.Region}}".
Signal documentation_gap = {{.Value}}.

DATA/CODE:
{{.Code}}

Find documentation gaps: missing data dictionary entries, undocumented transformations, unclear column meanings. Output numbered findings.`,
			},
		},
		Seeder:      func(input string) (*field.GradientField, error) { return scan.Dir(input) },
		ToolBuilder: func(input string) *tool.Registry { return tool.DefaultRegistry(input) },
	}
}
