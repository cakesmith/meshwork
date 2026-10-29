// Package distributed owns bindings for the distributed example, as a consuming application would.
package distributed

import (
	"example.com/meshwork-prototype/examples/distributed/primitives/fanout"
	"example.com/meshwork-prototype/examples/distributed/primitives/implementation"
	"example.com/meshwork-prototype/meshwork"
)

// Implementations binds the example's adjacent transition source files for execution.
func Implementations() map[string]meshwork.Compute {
	return map[string]meshwork.Compute{
		"logic.nand.go":      implementation.NAND,
		"logic.duplicate.go": fanout.Duplicate,
	}
}
