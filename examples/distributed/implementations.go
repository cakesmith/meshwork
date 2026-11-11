// Package distributed owns bindings for the distributed example, as a consuming application would.
package distributed

import (
	"github.com/cakesmith/meshwork/examples/distributed/primitives/fanout"
	"github.com/cakesmith/meshwork/examples/distributed/primitives/implementation"
	"github.com/cakesmith/meshwork/meshwork"
)

// Implementations binds the example's adjacent transition source files for execution.
func Implementations() map[string]meshwork.Compute {
	return map[string]meshwork.Compute{
		"logic.nand.go":      implementation.NAND,
		"logic.duplicate.go": fanout.Duplicate,
	}
}
