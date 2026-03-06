// Morphogen chemical vocabulary for stigmergic agent-to-agent
// communication through the gradient field. Agents secrete these
// chemicals, creating concentration gradients that neighbors sense.

package agent

// Chemical types secreted by agents into the gradient field.
const (
	Presence   = "presence"   // role-keyed, enables lateral inhibition
	Finding    = "finding"    // attracts complementary roles
	Saturation = "saturation" // role-keyed, discourages redundant work
	Distress   = "distress"   // spreads fast, recruits help, triggers division
	Nutrient   = "nutrient"   // released on death, triggers recruitment
)

// Keyed returns a role-specific chemical key, e.g. "presence:bug_hunter".
func Keyed(chemical, role string) string {
	return chemical + ":" + role
}
