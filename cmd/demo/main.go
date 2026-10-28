package main

import (
	"context"
	"errors"
	"example.com/meshwork-prototype/meshwork"
	"fmt"
	"log"
)

func main() {
	if err := demo(); err != nil {
		log.Fatal(err)
	}
}
func demo() error {
	library, impl := meshwork.LogicLibrary()
	graph, err := library.Flatten(meshwork.ORGraph(), "/logic")
	if err != nil {
		return err
	}
	for _, a := range []bool{false, true} {
		for _, b := range []bool{false, true} {
			r, err := graph.Instantiate(impl)
			if err != nil {
				return err
			}
			for name, value := range map[string]bool{"/logic/A": a, "/logic/B": b} {
				tok, err := meshwork.NewToken("Bool", value)
				if err != nil {
					return err
				}
				if err := r.Put(name, tok); err != nil {
					return err
				}
			}
			// This acyclic example fires each transition once; it is not a scheduler.
			pending := append([]meshwork.AtomicTransition(nil), graph.Transitions...)
			for len(pending) > 0 {
				var next []meshwork.AtomicTransition
				for _, t := range pending {
					err := r.Fire(context.Background(), t.Name)
					if errors.Is(err, meshwork.ErrNotEnabled) {
						next = append(next, t)
					} else if err != nil {
						return err
					}
				}
				if len(next) == len(pending) {
					return fmt.Errorf("example made no progress")
				}
				pending = next
			}
			q, ok := r.Token("/logic/Q")
			if !ok {
				return fmt.Errorf("missing OR result")
			}
			var value bool
			if err := q.Decode(&value); err != nil {
				return err
			}
			fmt.Printf("%t OR %t = %t (%d transitions)\n", a, b, value, len(graph.Transitions))
		}
	}
	return nil
}
