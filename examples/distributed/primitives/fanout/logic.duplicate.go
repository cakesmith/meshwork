package fanout

import (
	"context"

	"github.com/cakesmith/meshwork/meshwork"
)

// Duplicate implements the adjacent graph.duplicate.meshwork.yaml transition.
func Duplicate(_ context.Context, in map[string]meshwork.Token) (map[string]meshwork.Token, error) {
	return map[string]meshwork.Token{"L": in["A"].Clone(), "R": in["A"].Clone()}, nil
}
