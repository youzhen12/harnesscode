package agents

import _ "embed"

// Embedded agent prompt files.

//go:embed orchestrator.md
var Orchestrator string

//go:embed initializer.md
var Initializer string

//go:embed coder.md
var Coder string

//go:embed tester.md
var Tester string

//go:embed fixer.md
var Fixer string

//go:embed reviewer.md
var Reviewer string

// All returns file name -> content mapping.
func All() map[string]string {
	return map[string]string{
		"orchestrator.md": Orchestrator,
		"initializer.md":  Initializer,
		"coder.md":        Coder,
		"tester.md":       Tester,
		"fixer.md":        Fixer,
		"reviewer.md":     Reviewer,
	}
}
