package field

import (
	"fmt"
	"math"
	"sync"
)

// Signal represents a named dimension in the gradient field.
type Signal string

const (
	Complexity Signal = "complexity"
	BugDensity Signal = "bug_density"
	TestCoverage Signal = "test_coverage"
	Security   Signal = "security"
	Performance Signal = "performance"
	DocDebt    Signal = "doc_debt"
)

// AllSignals is the canonical list of signals used by the system.
var AllSignals = []Signal{Complexity, BugDensity, TestCoverage, Security, Performance, DocDebt}

// Point represents a location in the gradient field (e.g. a code region).
type Point struct {
	ID      string
	Signals map[Signal]float64
	Links   []string // IDs of linked points for diffusion
}

// GradientField is a thread-safe multi-dimensional signal space.
type GradientField struct {
	mu     sync.RWMutex
	points map[string]*Point
}

// New creates an empty gradient field.
func New() *GradientField {
	return &GradientField{points: make(map[string]*Point)}
}

// AddPoint adds or replaces a point in the field.
func (f *GradientField) AddPoint(p *Point) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.points[p.ID] = p
}

// Point returns a snapshot of the point with the given ID.
func (f *GradientField) Point(id string) (Point, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	p, ok := f.points[id]
	if !ok {
		return Point{}, false
	}
	// Return a copy of signals.
	cp := Point{ID: p.ID, Links: p.Links, Signals: make(map[Signal]float64, len(p.Signals))}
	for k, v := range p.Signals {
		cp.Signals[k] = v
	}
	return cp, true
}

// Points returns a snapshot of all point IDs.
func (f *GradientField) Points() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	ids := make([]string, 0, len(f.points))
	for id := range f.points {
		ids = append(ids, id)
	}
	return ids
}

// ReadSignal returns the signal value at a point.
func (f *GradientField) ReadSignal(pointID string, sig Signal) float64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	p, ok := f.points[pointID]
	if !ok {
		return 0
	}
	return p.Signals[sig]
}

// AddSignal atomically adds delta to a signal at a point.
func (f *GradientField) AddSignal(pointID string, sig Signal, delta float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.points[pointID]
	if !ok {
		return
	}
	p.Signals[sig] = math.Max(0, p.Signals[sig]+delta)
}

// Decay multiplies all signals by (1 - rate). Rate should be in [0, 1].
func (f *GradientField) Decay(rate float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.points {
		for sig, val := range p.Signals {
			p.Signals[sig] = val * (1 - rate)
			if p.Signals[sig] < 0.01 {
				p.Signals[sig] = 0
			}
		}
	}
}

// Diffuse spreads signals between linked points. diffusionRate in [0, 1].
func (f *GradientField) Diffuse(diffusionRate float64) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Compute deltas from a snapshot.
	deltas := make(map[string]map[Signal]float64)
	for id, p := range f.points {
		deltas[id] = make(map[Signal]float64)
		for _, linkID := range p.Links {
			neighbor, ok := f.points[linkID]
			if !ok {
				continue
			}
			for sig, val := range p.Signals {
				nval := neighbor.Signals[sig]
				diff := (val - nval) * diffusionRate * 0.5
				deltas[id][sig] -= diff
				if deltas[linkID] == nil {
					deltas[linkID] = make(map[Signal]float64)
				}
				deltas[linkID][sig] += diff
			}
		}
	}

	// Apply deltas.
	for id, sigDeltas := range deltas {
		p := f.points[id]
		for sig, d := range sigDeltas {
			p.Signals[sig] = math.Max(0, p.Signals[sig]+d)
		}
	}
}

// Snapshot returns a human-readable summary of the field.
func (f *GradientField) Snapshot() string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := ""
	for _, p := range f.points {
		out += fmt.Sprintf("  [%s]", p.ID)
		for _, sig := range AllSignals {
			if v := p.Signals[sig]; v > 0 {
				out += fmt.Sprintf(" %s=%.2f", sig, v)
			}
		}
		out += "\n"
	}
	return out
}
