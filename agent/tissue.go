package agent

import (
	"fmt"
	"strings"
)

// Cluster represents an organ-like group of co-located agents.
type Cluster struct {
	PointID string
	Agents  []*Agent
	Roles   map[Role]int
}

func (c *Cluster) String() string {
	var parts []string
	for role, count := range c.Roles {
		parts = append(parts, fmt.Sprintf("%s:%d", role, count))
	}
	return fmt.Sprintf("Tissue[%s] agents=%d {%s}", c.PointID, len(c.Agents), strings.Join(parts, ", "))
}

// TissueDetector groups co-located living agents into tissue clusters.
type TissueDetector struct{}

// NewTissueDetector creates a tissue detector.
func NewTissueDetector() *TissueDetector {
	return &TissueDetector{}
}

// Detect finds clusters of 2+ alive agents at the same point.
func (d *TissueDetector) Detect(agents []*Agent) []*Cluster {
	byPoint := make(map[string][]*Agent)
	for _, a := range agents {
		if a.Phase != Apoptotic {
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
			Roles:   make(map[Role]int),
		}
		for _, a := range group {
			c.Roles[a.Role]++
		}
		clusters = append(clusters, c)
	}
	return clusters
}
