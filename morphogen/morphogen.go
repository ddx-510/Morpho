package morphogen

import (
	"fmt"

	"github.com/ddx-510/Morpho/field"
)

// Kind describes the type of morphogen signal.
type Kind int

const (
	PRESENCE   Kind = iota // An agent is working here
	NEED                   // More help needed
	SATURATION             // Too many agents / signal is handled
	ALARM                  // Critical issue detected
)

func (k Kind) String() string {
	switch k {
	case PRESENCE:
		return "PRESENCE"
	case NEED:
		return "NEED"
	case SATURATION:
		return "SATURATION"
	case ALARM:
		return "ALARM"
	}
	return "UNKNOWN"
}

// Signal is a morphogen emitted into the gradient field.
type Signal struct {
	Kind    Kind
	Source  string       // agent ID that emitted it
	PointID string       // target point in the field
	Channel field.Signal // which signal dimension to affect
	Value   float64      // intensity
}

func (s Signal) String() string {
	return fmt.Sprintf("[%s] %s @ %s %s=%.2f", s.Kind, s.Source, s.PointID, s.Channel, s.Value)
}

// Bus is a stigmergic signaling bus that reshapes the gradient field.
type Bus struct {
	queue []Signal
}

// NewBus creates a new morphogen bus.
func NewBus() *Bus {
	return &Bus{}
}

// Emit queues a morphogen signal.
func (b *Bus) Emit(s Signal) {
	b.queue = append(b.queue, s)
}

// Flush applies all queued signals to the gradient field and clears the queue.
func (b *Bus) Flush(f *field.GradientField) []Signal {
	applied := b.queue
	for _, s := range b.queue {
		switch s.Kind {
		case PRESENCE:
			// Slight suppression: signals presence so others diffuse away.
			f.AddSignal(s.PointID, s.Channel, -s.Value*0.1)
		case NEED:
			// Amplify the signal to attract agents.
			f.AddSignal(s.PointID, s.Channel, s.Value)
		case SATURATION:
			// Suppress the signal to repel agents.
			f.AddSignal(s.PointID, s.Channel, -s.Value)
		case ALARM:
			// Strongly amplify to attract immediate attention.
			f.AddSignal(s.PointID, s.Channel, s.Value*2)
		}
	}
	b.queue = b.queue[:0]
	return applied
}

// Pending returns the number of queued signals.
func (b *Bus) Pending() int {
	return len(b.queue)
}
