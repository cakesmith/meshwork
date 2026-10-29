package implementation

import (
	"context"

	"example.com/meshwork-prototype/meshwork"
)

// NAND implements the adjacent graph.nand.meshwork.yaml transition.
func NAND(_ context.Context, in map[string]meshwork.Token) (map[string]meshwork.Token, error) {
	var a, b bool
	if err := in["A"].Decode(&a); err != nil {
		return nil, err
	}
	if err := in["B"].Decode(&b); err != nil {
		return nil, err
	}
	token, err := meshwork.NewToken("Bool", !(a && b))
	return map[string]meshwork.Token{"Q": token}, err
}
