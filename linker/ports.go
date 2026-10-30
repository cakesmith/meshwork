package linker

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// placeType keeps the scalar shorthand while accepting an explicit definition.
type placeType string

func (p *placeType) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode && n.Tag == "!!str" {
		*p = placeType(n.Value)
		return nil
	}
	if err := fields(n, "type"); err != nil {
		return fmt.Errorf("place: %w", err)
	}
	var value struct {
		Type string `yaml:"type"`
	}
	if err := n.Decode(&value); err != nil {
		return err
	}
	if value.Type == "" {
		return fmt.Errorf("place type is required")
	}
	*p = placeType(value.Type)
	return nil
}

type arcSpec struct {
	From      string `yaml:"from"`
	To        string `yaml:"to"`
	Port      string `yaml:"port,omitempty"`
	Transport string `yaml:"transport,omitempty"`
}

func (a *arcSpec) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.SequenceNode {
		var pair []string
		if err := n.Decode(&pair); err != nil {
			return err
		}
		if len(pair) != 2 {
			return fmt.Errorf("arc must have two endpoints")
		}
		a.From, a.To = pair[0], pair[1]
	} else {
		if err := fields(n, "from to port transport"); err != nil {
			return fmt.Errorf("arc: %w", err)
		}
		type plain arcSpec
		if err := n.Decode((*plain)(a)); err != nil {
			return err
		}
		for i := 0; i < len(n.Content); i += 2 {
			if (n.Content[i].Value == "port" || n.Content[i].Value == "transport") && n.Content[i+1].Value == "" {
				return fmt.Errorf("arc %s must not be empty", n.Content[i].Value)
			}
		}
	}
	if a.From == "" || a.To == "" {
		return fmt.Errorf("arc endpoints are required")
	}
	if strings.ContainsAny(a.Port, "/\\ \t\r\n\x00") {
		return fmt.Errorf("invalid local port %q", a.Port)
	}
	if strings.ContainsAny(a.Transport, " \t\r\n\x00") {
		return fmt.Errorf("invalid arc transport identity %q", a.Transport)
	}
	return nil
}

// Node.Decode does not inherit KnownFields from the enclosing decoder.
func fields(n *yaml.Node, allowed string) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("expected a mapping")
	}
	seen := map[string]bool{}
	for i := 0; i < len(n.Content); i += 2 {
		key := n.Content[i].Value
		if seen[key] || !strings.Contains(" "+allowed+" ", " "+key+" ") {
			return fmt.Errorf("unknown or duplicate field %q", key)
		}
		seen[key] = true
	}
	return nil
}
