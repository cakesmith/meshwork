package meshwork

import "context"

// testLogicLibrary is private test data, not a Meshwork-supplied application library.
func testLogicLibrary() (Library, map[string]Compute) {
	nand := Graph{Places: []Place{{"A", "Bool"}, {"B", "Bool"}, {"Q", "Bool"}}, Transitions: []AtomicTransition{{Name: "nand", Implementation: "test.nand", Inputs: []string{"A", "B"}, Outputs: []string{"Q"}, InputPorts: []string{"A", "B"}, OutputPorts: []string{"Q"}}}}
	dup := Graph{Places: []Place{{"A", "Bool"}, {"L", "Bool"}, {"R", "Bool"}}, Transitions: []AtomicTransition{{Name: "dup", Implementation: "test.duplicate", Inputs: []string{"A"}, Outputs: []string{"L", "R"}, InputPorts: []string{"A"}, OutputPorts: []string{"L", "R"}}}}
	not := Graph{Places: []Place{{"A", "Bool"}, {"Q", "Bool"}, {"left", "Bool"}, {"right", "Bool"}}, References: []Reference{
		{Graph: "/logic/duplicate", Namespace: "dup", Bindings: map[string]string{"A": "A", "L": "left", "R": "right"}},
		{Graph: "/logic/nand", Namespace: "nand", Bindings: map[string]string{"A": "left", "B": "right", "Q": "Q"}},
	}}
	or := Graph{Places: []Place{{"A", "Bool"}, {"B", "Bool"}, {"Q", "Bool"}, {"notA", "Bool"}, {"notB", "Bool"}}, References: []Reference{
		{Graph: "/logic/not", Namespace: "invertA", Bindings: map[string]string{"A": "A", "Q": "notA"}},
		{Graph: "/logic/not", Namespace: "invertB", Bindings: map[string]string{"A": "B", "Q": "notB"}},
		{Graph: "/logic/nand", Namespace: "nand", Bindings: map[string]string{"A": "notA", "B": "notB", "Q": "Q"}},
	}}
	return Library{Graphs: map[string]Graph{"/logic/nand": nand, "/logic/duplicate": dup, "/logic/not": not, "/logic/or": or}}, map[string]Compute{
		"test.nand": func(_ context.Context, in map[string]Token) (map[string]Token, error) {
			var a, b bool
			if err := in["A"].Decode(&a); err != nil {
				return nil, err
			}
			if err := in["B"].Decode(&b); err != nil {
				return nil, err
			}
			token, err := NewToken("Bool", !(a && b))
			return map[string]Token{"Q": token}, err
		},
		"test.duplicate": func(_ context.Context, in map[string]Token) (map[string]Token, error) {
			return map[string]Token{"L": in["A"].Clone(), "R": in["A"].Clone()}, nil
		},
	}
}

func testORGraph() Graph {
	return Graph{Places: []Place{{"A", "Bool"}, {"B", "Bool"}, {"Q", "Bool"}}, References: []Reference{
		{Namespace: "or", Graph: "/logic/or", Bindings: map[string]string{"A": "A", "B": "B", "Q": "Q"}},
	}}
}
