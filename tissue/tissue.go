package tissue

import (
	"fmt"
	"strings"

	"github.com/ddx-510/Morpho/agent"
)

// Cluster represents an organ-like group of co-located agents.
type Cluster struct {
	PointID string
	Agents  []*agent.Agent
	Roles   map[agent.Role]int
}

func (c *Cluster) String() string {
	var parts []string
	for role, count := range c.Roles {
		parts = append(parts, fmt.Sprintf("%s:%d", role, count))
	}
	return fmt.Sprintf("Tissue[%s] agents=%d {%s}", c.PointID, len(c.Agents), strings.Join(parts, ", "))
}

// Detector groups co-located living agents into tissue clusters.
type Detector struct{}

// NewDetector creates a tissue detector.
func NewDetector() *Detector {
	return &Detector{}
}

// Detect finds clusters of 2+ agents at the same point.
func (d *Detector) Detect(agents []*agent.Agent) []*Cluster {
	byPoint := make(map[string][]*agent.Agent)
	for _, a := range agents {
		if a.State == agent.Alive {
			byPoint[a.PointID] = append(byPoint[a.PointID], a)
		}
	}

	var clusters []*Cluster
	for pointID, group := range byPoint {
		if len(group) < 2 {
			continue
		}
		c := &Cluster{
			PointID: pointID,
			Agents:  group,
			Roles:   make(map[agent.Role]int),
		}
		for _, a := range group {
			c.Roles[a.Role]++
		}
		clusters = append(clusters, c)
	}
	return clusters
}
