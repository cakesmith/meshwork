package meshwork

import "context"

// LogicLibrary contains reusable architectures, including explicit token fan-out.
func LogicLibrary() (Library, map[string]Compute) {
	binary := Entity{Inputs: []Place{{"A", "Bool"}, {"B", "Bool"}}, Outputs: []Place{{"Q", "Bool"}}}
	unary := Entity{Inputs: []Place{{"A", "Bool"}}, Outputs: []Place{{"Q", "Bool"}}}
	dup := Entity{Inputs: []Place{{"A", "Bool"}}, Outputs: []Place{{"L", "Bool"}, {"R", "Bool"}}}
	not := Graph{
		Places: []Place{{"A", "Bool"}, {"Q", "Bool"}, {"left", "Bool"}, {"right", "Bool"}},
		References: []Reference{
			{Name: "dup", Architecture: "logic.duplicate:go", Bindings: map[string]string{"A": "A", "L": "left", "R": "right"}},
			{Name: "nand", Architecture: "logic.nand:go", Bindings: map[string]string{"A": "left", "B": "right", "Q": "Q"}},
		},
	}
	or := Graph{
		Places: []Place{{"A", "Bool"}, {"B", "Bool"}, {"Q", "Bool"}, {"notA", "Bool"}, {"notB", "Bool"}},
		References: []Reference{
			{Name: "invertA", Architecture: "logic.not:nand", Bindings: map[string]string{"A": "A", "Q": "notA"}},
			{Name: "invertB", Architecture: "logic.not:nand", Bindings: map[string]string{"A": "B", "Q": "notB"}},
			{Name: "nand", Architecture: "logic.nand:go", Bindings: map[string]string{"A": "notA", "B": "notB", "Q": "Q"}},
		},
	}
	l := Library{
		Entities: map[string]Entity{"logic.nand": binary, "logic.not": unary, "logic.or": binary, "logic.duplicate": dup},
		Architectures: map[string]Architecture{
			"logic.nand:go":      {Entity: "logic.nand", Implementation: "logic.nand.go"},
			"logic.duplicate:go": {Entity: "logic.duplicate", Implementation: "logic.duplicate.go"},
			"logic.not:nand":     {Entity: "logic.not", Graph: &not},
			"logic.or:nand":      {Entity: "logic.or", Graph: &or},
		},
	}
	impl := map[string]Compute{
		"logic.nand.go": func(_ context.Context, in map[string]Token) (map[string]Token, error) {
			var a, b bool
			if err := in["A"].Decode(&a); err != nil {
				return nil, err
			}
			if err := in["B"].Decode(&b); err != nil {
				return nil, err
			}
			tok, err := NewToken("Bool", !(a && b))
			return map[string]Token{"Q": tok}, err
		},
		"logic.duplicate.go": func(_ context.Context, in map[string]Token) (map[string]Token, error) {
			return map[string]Token{"L": in["A"].Clone(), "R": in["A"].Clone()}, nil
		},
	}
	return l, impl
}

// ORGraph binds a reusable OR architecture to three external typed places.
func ORGraph() Graph {
	return Graph{Places: []Place{{"A", "Bool"}, {"B", "Bool"}, {"Q", "Bool"}}, References: []Reference{
		{Name: "or", Architecture: "logic.or:nand", Bindings: map[string]string{"A": "A", "B": "B", "Q": "Q"}},
	}}
}
