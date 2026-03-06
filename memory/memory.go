package memory

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ── Tissue Memory (epigenetic) ──────────────────────────────────────
// Persists field state across sessions. When a swarm analyzes a codebase,
// the findings and signal levels are saved. Next time the same regions
// appear, agents start with residual knowledge instead of blank slate.
//
// Analogy: epigenetic marks — the organism's cells died, but the tissue
// retains chemical imprints that influence future cell behavior.

// RegionMemory is the persisted knowledge about one field region.
type RegionMemory struct {
	RegionID   string             `json:"region_id"`
	Signals    map[string]float64 `json:"signals"`              // last known signal values
	Findings   []string           `json:"findings"`             // accumulated findings
	LastSeen   time.Time          `json:"last_seen"`
	RunCount   int                `json:"run_count"`            // how many swarm runs touched this region
	FindingSet map[string]bool    `json:"finding_hashes"`       // dedup hashes
}

// TissueMemory is the organism-level persistent field memory.
type TissueMemory struct {
	mu      sync.RWMutex
	path    string
	Regions map[string]*RegionMemory `json:"regions"`
}

// NewTissueMemory creates or loads tissue memory from disk.
func NewTissueMemory(path string) *TissueMemory {
	tm := &TissueMemory{
		path:    path,
		Regions: make(map[string]*RegionMemory),
	}
	tm.load()
	return tm
}

func (tm *TissueMemory) load() {
	data, err := os.ReadFile(tm.path)
	if err != nil {
		return
	}
	json.Unmarshal(data, &tm.Regions)
}

func (tm *TissueMemory) save() error {
	if tm.path == "" {
		return nil
	}
	dir := filepath.Dir(tm.path)
	if dir != "." && dir != "" {
		os.MkdirAll(dir, 0755)
	}
	data, err := json.MarshalIndent(tm.Regions, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(tm.path, data, 0644)
}

// Absorb ingests findings and signals from a completed swarm run.
// Signal values are stored with time-decay: older values fade.
func (tm *TissueMemory) Absorb(regionID string, signals map[string]float64, findings []string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	rm, ok := tm.Regions[regionID]
	if !ok {
		rm = &RegionMemory{
			RegionID:   regionID,
			Signals:    make(map[string]float64),
			FindingSet: make(map[string]bool),
		}
		tm.Regions[regionID] = rm
	}

	// Merge signals: blend old (decayed) with new.
	for sig, val := range signals {
		old := rm.Signals[sig]
		rm.Signals[sig] = old*0.3 + val*0.7 // new dominates
	}

	// Add findings with dedup.
	for _, f := range findings {
		hash := findingHash(f)
		if rm.FindingSet[hash] {
			continue
		}
		rm.FindingSet[hash] = true
		rm.Findings = append(rm.Findings, f)
	}

	// Cap findings per region to prevent unbounded growth.
	if len(rm.Findings) > 50 {
		rm.Findings = rm.Findings[len(rm.Findings)-50:]
	}

	rm.LastSeen = time.Now()
	rm.RunCount++
	tm.save()
}

// Recall returns prior knowledge for a region: residual signals and past findings.
// Signals are decayed based on time since last seen.
func (tm *TissueMemory) Recall(regionID string) (signals map[string]float64, findings []string) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	rm, ok := tm.Regions[regionID]
	if !ok {
		return nil, nil
	}

	// Time-decay: signals fade over days.
	daysSince := time.Since(rm.LastSeen).Hours() / 24
	decay := math.Exp(-0.1 * daysSince) // half-life ~7 days

	signals = make(map[string]float64)
	for sig, val := range rm.Signals {
		decayed := val * decay
		if decayed > 0.01 {
			signals[sig] = decayed
		}
	}

	findings = rm.Findings
	return signals, findings
}

// AllRegions returns all region IDs in tissue memory.
func (tm *TissueMemory) AllRegions() []string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	ids := make([]string, 0, len(tm.Regions))
	for id := range tm.Regions {
		ids = append(ids, id)
	}
	return ids
}

// RegionCount returns the number of remembered regions.
func (tm *TissueMemory) RegionCount() int {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return len(tm.Regions)
}

// findingHash produces a simple content hash for dedup.
// Uses first 100 chars lowercased + length — cheap but effective.
func findingHash(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if len(s) > 100 {
		s = s[:100]
	}
	return s
}

// ── TF-IDF Scoring ──────────────────────────────────────────────────
// Used by FactStore.Relevant() for better retrieval than substring.

// tfidfScore computes a relevance score between a query and a document.
// Both are plain text. Returns 0-1 normalized score.
func tfidfScore(query, doc string) float64 {
	queryTerms := tokenize(query)
	docTerms := tokenize(doc)
	if len(queryTerms) == 0 || len(docTerms) == 0 {
		return 0
	}

	// Term frequency in document.
	docTF := make(map[string]int)
	for _, t := range docTerms {
		docTF[t]++
	}

	// Score: sum of (tf * idf-proxy) for each query term found in doc.
	// IDF proxy: rarer terms in the doc get higher weight.
	var score float64
	docLen := float64(len(docTerms))
	for _, qt := range queryTerms {
		tf := float64(docTF[qt]) / docLen
		if tf > 0 {
			// IDF proxy: penalize very common terms.
			idf := 1.0 + math.Log(docLen/float64(docTF[qt]))
			score += tf * idf
		}
	}

	// Normalize by query length.
	return math.Min(score/float64(len(queryTerms)), 1.0)
}

func tokenize(s string) []string {
	s = strings.ToLower(s)
	words := strings.FieldsFunc(s, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_')
	})
	// Filter stopwords and very short tokens.
	var result []string
	for _, w := range words {
		if len(w) >= 2 && !isStopword(w) {
			result = append(result, w)
		}
	}
	return result
}

var stopwords = map[string]bool{
	"the": true, "is": true, "at": true, "in": true, "of": true,
	"and": true, "or": true, "to": true, "for": true, "it": true,
	"this": true, "that": true, "with": true, "from": true, "on": true,
	"an": true, "be": true, "as": true, "are": true, "was": true,
	"has": true, "have": true, "had": true, "not": true, "but": true,
}

func isStopword(w string) bool { return stopwords[w] }
