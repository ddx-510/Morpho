package domain

// Builtin returns all built-in domain definitions.
var Builtin = map[string]func() *Domain{
	"code_review":    CodeReview,
	"research":       Research,
	"writing_review": WritingReview,
	"data_analysis":  DataAnalysis,
}

// Get returns a built-in domain by name.
func Get(name string) (*Domain, bool) {
	fn, ok := Builtin[name]
	if !ok {
		return nil, false
	}
	return fn(), true
}

// Names returns all built-in domain names.
func Names() []string {
	names := make([]string, 0, len(Builtin))
	for name := range Builtin {
		names = append(names, name)
	}
	return names
}
