package linker

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"example.com/meshwork-prototype/meshwork"
	"gopkg.in/yaml.v3"
)

const MaxGraphMLBytes = 8 << 20
const graphMLNS = "http://graphml.graphdrawing.org/xmlns"
const yedNS = "http://www.yworks.com/xml/graphml"

// GraphMLDocument keeps presentation and source identity outside the computational graph.
type GraphMLDocument struct {
	Filename   string
	XML        string
	Provenance []Provenance
	ArcIDs     []string
	Nodes      map[string]VisualNode
}
type VisualNode struct {
	Label, Shape string
	Geometry     *Geometry `json:",omitempty"`
	Group        bool
}
type Geometry struct{ X, Y, Width, Height string }
type Provenance struct {
	ID, Identity, Convention string
	Properties               map[string]string
}
type xmlElement struct {
	Name     xml.Name
	Attr     []xml.Attr
	Text     strings.Builder
	Children []*xmlElement
}

func (e *xmlElement) attr(name string) string {
	for _, a := range e.Attr {
		if a.Name.Space == "" && a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}
func (e *xmlElement) children(name string) []*xmlElement {
	var out []*xmlElement
	for _, c := range e.Children {
		if c.Name.Space == graphMLNS && c.Name.Local == name {
			out = append(out, c)
		}
	}
	return out
}
func (e *xmlElement) visual(name string) string {
	if e.Name.Space == yedNS && e.Name.Local == name {
		if name == "Shape" {
			return e.attr("type")
		}
		return strings.TrimSpace(e.Text.String())
	}
	for _, c := range e.Children {
		if c.Name.Space == graphMLNS && c.Name.Local == "graph" {
			continue
		}
		if v := c.visual(name); v != "" {
			return v
		}
	}
	return ""
}
func (e *xmlElement) geometry() *Geometry {
	if e.Name.Space == yedNS && e.Name.Local == "Geometry" {
		return &Geometry{e.attr("x"), e.attr("y"), e.attr("width"), e.attr("height")}
	}
	for _, c := range e.Children {
		if c.Name.Space == graphMLNS && c.Name.Local == "graph" {
			continue
		}
		if g := c.geometry(); g != nil {
			return g
		}
	}
	return nil
}
func readXML(b []byte) (*xmlElement, error) {
	d := xml.NewDecoder(strings.NewReader(strings.TrimPrefix(string(b), "\uFEFF"))) // No Entity map or external resolver: external entities cannot expand.
	var root *xmlElement
	var stack []*xmlElement
	count := 0
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.Directive:
			return nil, fmt.Errorf("XML directives/DOCTYPE are unsupported")
		case xml.StartElement:
			count++
			if count > 100000 || len(stack) >= 128 {
				return nil, fmt.Errorf("XML element/depth limit exceeded")
			}
			attrs := map[xml.Name]bool{}
			for _, a := range t.Attr {
				if attrs[a.Name] {
					return nil, fmt.Errorf("duplicate XML attribute %s", a.Name.Local)
				}
				attrs[a.Name] = true
			}
			e := &xmlElement{Name: t.Name, Attr: t.Attr}
			if len(stack) == 0 {
				if root != nil {
					return nil, fmt.Errorf("expected one GraphML document")
				}
				root = e
			} else {
				p := stack[len(stack)-1]
				p.Children = append(p.Children, e)
			}
			stack = append(stack, e)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].Text.Write(t)
			} else if strings.TrimSpace(string(t)) != "" {
				return nil, fmt.Errorf("text outside GraphML root")
			}
		}
	}
	if root == nil || root.Name != (xml.Name{Space: graphMLNS, Local: "graphml"}) {
		return nil, fmt.Errorf("expected GraphML root with standard namespace")
	}
	return root, nil
}

type graphMLKey struct {
	Name, Scope, Type, Default string
	HasDefault                 bool
}
type semanticProperties struct{ Kind, Name, Namespace, Type, Implementation, Timeout string }

func semantics(p map[string]string) (semanticProperties, error) {
	var s semanticProperties
	fields := map[string]*string{"meshwork.kind": &s.Kind, "meshwork.name": &s.Name, "meshwork.namespace": &s.Namespace, "meshwork.type": &s.Type, "meshwork.implementation": &s.Implementation, "meshwork.timeout": &s.Timeout}
	for _, k := range sortedKeys(p) {
		v := p[k]
		if !strings.HasPrefix(k, "meshwork.") {
			continue
		}
		target, ok := fields[k]
		if !ok {
			return s, fmt.Errorf("unknown semantic property %q", k)
		}
		*target = strings.TrimSpace(v)
	}
	return s, nil
}

// ImportGraphML uses explicit properties by default. yEd shapes/labels require conventions=true.
func ImportGraphML(filename string, conventions bool) (Result, error) {
	p, err := filepath.Abs(filename)
	if err != nil {
		return Result{}, err
	}
	kind := "graphml"
	if conventions {
		kind = "graphml-yed"
	}
	return LinkFiles([]File{{Path: p, Kind: kind, Name: strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)), Repository: Repository{Name: "import", Root: filepath.Dir(p), Namespace: "/"}}})
}

// DiscoverGraphML is an explicit opt-in, separate from ordinary YAML discovery.
func DiscoverGraphML(repos []Repository, conventions bool) ([]File, error) {
	// Reuse repository validation and YAML discovery's symlink policy.
	if _, err := Discover(repos); err != nil {
		return nil, err
	}
	var files []File
	for _, repo := range repos {
		root, err := filepath.Abs(repo.Root)
		if err != nil {
			return nil, err
		}
		repo.Root = root
		err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if p != root && (d.Name() == ".git" || d.Name() == ".cache") {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.EqualFold(filepath.Ext(p), ".graphml") {
				if d.Type()&os.ModeSymlink != 0 {
					return fmt.Errorf("GraphML source symlink is unsupported: %s", p)
				}
				kind := "graphml"
				if conventions {
					kind = "graphml-yed"
				}
				files = append(files, File{p, kind, strings.TrimSuffix(d.Name(), filepath.Ext(p)), repo})
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}
func parseGraphML(f File) (_ *source, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("%s: %w", f.Path, err)
		}
	}()
	file, err := os.Open(f.Path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	b, err := io.ReadAll(io.LimitReader(file, MaxGraphMLBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxGraphMLBytes {
		return nil, fmt.Errorf("GraphML exceeds %d byte limit", MaxGraphMLBytes)
	}
	root, err := readXML(b)
	if err != nil {
		return nil, err
	}
	keys := map[string]graphMLKey{}
	for _, k := range root.children("key") {
		id := k.attr("id")
		if _, ok := keys[id]; id == "" || ok {
			return nil, fmt.Errorf("missing or duplicate key ID %q", id)
		}
		key := graphMLKey{Name: k.attr("attr.name"), Scope: k.attr("for"), Type: k.attr("attr.type")}
		if key.Scope == "" {
			key.Scope = "all"
		}
		defaults := k.children("default")
		if len(defaults) > 1 {
			return nil, fmt.Errorf("key %s has duplicate defaults", id)
		}
		if len(defaults) == 1 {
			key.Default = strings.TrimSpace(defaults[0].Text.String())
			key.HasDefault = true
		}
		if strings.HasPrefix(key.Name, "meshwork.") && key.Type != "string" {
			return nil, fmt.Errorf("key %s: Meshwork properties require attr.type=string", id)
		}
		keys[id] = key
	}
	properties := func(e *xmlElement, scope string) (map[string]string, error) {
		p := map[string]string{}
		seen := map[string]bool{}
		for _, id := range sortedKeys(keys) {
			k := keys[id]
			if k.HasDefault && (k.Scope == scope || k.Scope == "all") && k.Name != "" {
				if _, ok := p[k.Name]; ok {
					return nil, fmt.Errorf("duplicate property %s", k.Name)
				}
				p[k.Name] = k.Default
			}
		}
		names := map[string]bool{}
		for _, d := range e.children("data") {
			id := d.attr("key")
			k, ok := keys[id]
			if !ok {
				return nil, fmt.Errorf("%s: unknown data key %q", e.attr("id"), id)
			}
			if seen[id] || (k.Scope != scope && k.Scope != "all") {
				return nil, fmt.Errorf("%s: duplicate or wrong-scope key %s", e.attr("id"), id)
			}
			seen[id] = true
			if k.Name != "" {
				if names[k.Name] {
					return nil, fmt.Errorf("duplicate property %s", k.Name)
				}
				names[k.Name] = true
				if strings.HasPrefix(k.Name, "meshwork.") && len(d.Children) > 0 {
					return nil, fmt.Errorf("property %s must be text", k.Name)
				}
				p[k.Name] = strings.TrimSpace(d.Text.String())
			}
		}
		return p, nil
	}
	parent := f.Repository.Namespace
	if parent == "" {
		parent = "/"
	}
	ns, err := resolveName(parent, f.Repository.Name)
	if err != nil {
		return nil, err
	}
	s := &source{File: f, NS: ns, Doc: document{Places: map[string]placeType{}, Transitions: map[string]transition{}}, Imported: &GraphMLDocument{Filename: f.Path, XML: string(b), Nodes: map[string]VisualNode{}}}
	ids := map[string]bool{}
	nodeIDs := map[string]bool{}
	identities := map[string]string{}
	kinds := map[string]string{}
	claim := func(id string) error {
		if id == "" || ids[id] {
			return fmt.Errorf("missing or duplicate GraphML ID %q", id)
		}
		ids[id] = true
		return nil
	}
	type pendingEdge struct {
		Element  *xmlElement
		Directed bool
	}
	var edges []pendingEdge
	var walk func(*xmlElement, string) error
	walk = func(g *xmlElement, context string) error {
		p, err := properties(g, "graph")
		if err != nil {
			return err
		}
		sp, err := semantics(p)
		if err != nil {
			return err
		}
		if sp.Kind != "" || sp.Name != "" || sp.Type != "" || sp.Timeout != "" || sp.Implementation != "" {
			return fmt.Errorf("graph %s: only meshwork.namespace is supported on graphs", g.attr("id"))
		}
		if sp.Namespace != "" {
			context, err = resolveName(context, sp.Namespace)
			if err != nil {
				return err
			}
		}
		if len(s.Doc.Places) == 0 && len(s.Doc.Transitions) == 0 {
			s.NS = context
		}
		mode := g.attr("edgedefault")
		if mode != "directed" && mode != "undirected" {
			return fmt.Errorf("graph %s: edgedefault must be directed or undirected", g.attr("id"))
		}
		for _, n := range g.children("node") {
			id := n.attr("id")
			if err := claim(id); err != nil {
				return err
			}
			nodeIDs[id] = true
			p, err := properties(n, "node")
			if err != nil {
				return err
			}
			sp, err := semantics(p)
			if err != nil {
				return fmt.Errorf("node %s: %w", id, err)
			}
			prov := Provenance{ID: id, Properties: p}
			nested := n.children("graph")
			s.Imported.Nodes[id] = VisualNode{Label: n.visual("NodeLabel"), Shape: n.visual("Shape"), Geometry: n.geometry(), Group: len(nested) > 0}
			if len(nested) > 0 {
				if sp.Kind != "" && sp.Kind != "group" && sp.Kind != "annotation" {
					return fmt.Errorf("node %s: visual group cannot be computational", id)
				}
			} else {
				if sp.Kind == "" && f.Kind == "graphml-yed" {
					switch n.visual("Shape") {
					case "ellipse":
						sp.Kind = "place"
					case "rectangle":
						sp.Kind = "transition"
					}
					if sp.Kind != "" {
						prov.Convention = "yed-shape-label"
					}
				}
				switch sp.Kind {
				case "", "annotation", "group":
				case "place", "transition":
					if sp.Name == "" && prov.Convention != "" {
						sp.Name = n.visual("NodeLabel")
					}
					if sp.Name == "" {
						return fmt.Errorf("node %s: computational node requires meshwork.name", id)
					}
					nodeNS := context
					if sp.Namespace != "" {
						nodeNS, err = resolveName(context, sp.Namespace)
						if err != nil {
							return err
						}
					}
					name, err := resolveName(nodeNS, sp.Name)
					if err != nil {
						return fmt.Errorf("node %s: %w", id, err)
					}
					if name == "/" {
						return fmt.Errorf("node %s: root is not a computational identity", id)
					}
					if old := kinds[name]; old != "" && old != sp.Kind {
						return fmt.Errorf("node %s: conflicting identity %s", id, name)
					}
					kinds[name] = sp.Kind
					if sp.Kind == "place" {
						if sp.Timeout != "" || sp.Implementation != "" {
							return fmt.Errorf("node %s: place cannot declare transition properties", id)
						}
						old := s.Doc.Places[name]
						if old != "" && sp.Type != "" && string(old) != sp.Type {
							return fmt.Errorf("node %s: conflicting type for %s", id, name)
						}
						if sp.Type != "" || old == "" {
							s.Doc.Places[name] = placeType(sp.Type)
						}
					} else {
						if sp.Type != "" {
							return fmt.Errorf("node %s: transition cannot declare place type", id)
						}
						timeout, err := parseDuration(sp.Timeout)
						if err != nil {
							return fmt.Errorf("%s: %w", id, err)
						}
						t := transition{Implementation: sp.Implementation, Timeout: duration(timeout)}
						old, ok := s.Doc.Transitions[name]
						if ok {
							merge := func(a *string, b string) bool {
								if *a != "" && b != "" && *a != b {
									return false
								}
								if *a == "" {
									*a = b
								}
								return true
							}
							if !merge(&t.Implementation, old.Implementation) || t.Timeout != old.Timeout {
								return fmt.Errorf("node %s: conflicting transition %s", id, name)
							}
						}
						s.Doc.Transitions[name] = t
					}
					identities[id] = name
					prov.Identity = name
				default:
					return fmt.Errorf("node %s: unknown meshwork.kind %q", id, sp.Kind)
				}
			}
			s.Imported.Provenance = append(s.Imported.Provenance, prov)
			for _, child := range nested {
				if err := walk(child, context); err != nil {
					return err
				}
			}
		}
		for _, e := range g.children("edge") {
			id := e.attr("id")
			if id != "" {
				if err := claim(id); err != nil {
					return err
				}
			}
			directed := mode == "directed"
			switch e.attr("directed") {
			case "":
			case "true", "1":
				directed = true
			case "false", "0":
				directed = false
			default:
				return fmt.Errorf("edge %s: invalid directed attribute", id)
			}
			edges = append(edges, pendingEdge{e, directed})
		}
		if len(g.children("hyperedge")) > 0 {
			return fmt.Errorf("hyperedges are unsupported")
		}
		return nil
	}
	graphs := root.children("graph")
	if len(graphs) == 0 {
		return nil, fmt.Errorf("GraphML has no graph")
	}
	for _, g := range graphs {
		if err := walk(g, ns); err != nil {
			return nil, err
		}
	}
	for _, edge := range edges {
		e := edge.Element
		id := e.attr("id")
		a, b := e.attr("source"), e.attr("target")
		// Edge references must name nodes, including decorative nodes, never edge IDs.
		if !nodeIDs[a] || !nodeIDs[b] {
			return nil, fmt.Errorf("edge %s: invalid node reference %q -> %q", id, a, b)
		}
		p, err := properties(e, "edge")
		if err != nil {
			return nil, err
		}
		sp, err := semantics(p)
		if err != nil {
			return nil, err
		}
		if sp.Name != "" || sp.Namespace != "" || sp.Type != "" || sp.Timeout != "" || sp.Implementation != "" {
			return nil, fmt.Errorf("edge %s: only meshwork.kind is supported on edges", id)
		}
		from, to := identities[a], identities[b]
		prov := Provenance{ID: id, Properties: p}
		if sp.Kind != "" && sp.Kind != "annotation" && sp.Kind != "arc" {
			return nil, fmt.Errorf("edge %s: unsupported kind %s", id, sp.Kind)
		}
		if sp.Kind != "annotation" && (from != "" || to != "" || sp.Kind == "arc") {
			if from == "" || to == "" {
				return nil, fmt.Errorf("edge %s: computational arc touches noncomputational node", id)
			}
			if !edge.Directed {
				return nil, fmt.Errorf("edge %s: computational arcs must be directed", id)
			}
			if e.attr("sourceport") != "" || e.attr("targetport") != "" {
				return nil, fmt.Errorf("edge %s: GraphML ports are unsupported", id)
			}
			s.Doc.Arcs = append(s.Doc.Arcs, arcSpec{From: from, To: to})
			s.Imported.ArcIDs = append(s.Imported.ArcIDs, id)
			prov.Identity = from + " -> " + to
		}
		s.Imported.Provenance = append(s.Imported.Provenance, prov)
	}
	return s, nil
}

func incomplete(g meshwork.ExecutableGraph, declarations []declaration) []string {
	var ds []string
	locations := map[string][]string{}
	for _, d := range declarations {
		locations[d.Name] = append(locations[d.Name], d.Source)
	}
	add := func(name, message string) {
		sort.Strings(locations[name])
		ds = append(ds, fmt.Sprintf("%s: %s (%s)", name, message, strings.Join(dedup(locations[name]), ", ")))
	}
	for _, p := range g.Places {
		if p.Type == "" {
			add(p.Name, "missing place type")
		}
	}
	for _, t := range g.Transitions {
		if t.Implementation == "" {
			add(t.Name, "missing transition implementation")
		}
		if len(t.Inputs) == 0 || len(t.Outputs) == 0 {
			add(t.Name, "missing transition inputs/outputs")
		}
	}
	sort.Strings(ds)
	return ds
}

// CanonicalJSON excludes provenance, presentation and discovery handles.
func CanonicalJSON(g meshwork.ExecutableGraph) ([]byte, error) {
	g.Transitions = append([]meshwork.AtomicTransition(nil), g.Transitions...)
	for i := range g.Transitions {
		g.Transitions[i].Sources = nil
	}
	return json.Marshal(g)
}

// CanonicalYAML emits resolved names, so import context cannot change identity.
func CanonicalYAML(g meshwork.ExecutableGraph) ([]byte, error) {
	if _, err := diffIndex(Result{Graph: g}); err != nil {
		return nil, err
	}
	d := struct {
		Namespace   string                `yaml:"namespace"`
		Places      map[string]string     `yaml:"places"`
		Transitions map[string]transition `yaml:"transitions"`
		Arcs        []arcSpec             `yaml:"arcs"`
		References  []reference           `yaml:"references,omitempty"`
	}{Namespace: "/", Places: map[string]string{}, Transitions: map[string]transition{}}
	for _, p := range g.Places {
		d.Places[p.Name] = p.Type
	}
	for _, t := range g.Transitions {
		d.Transitions[t.Name] = transition{Implementation: t.Implementation, Timeout: duration(t.Timeout)}
		for i, p := range t.Inputs {
			name := port(p)
			if len(t.InputPorts) > 0 {
				name = t.InputPorts[i]
			}
			d.Arcs = append(d.Arcs, arcSpec{From: p, To: t.Name, Port: name, Transport: t.InputTransports[name]})
		}
		for i, p := range t.Outputs {
			name := port(p)
			if len(t.OutputPorts) > 0 {
				name = t.OutputPorts[i]
			}
			d.Arcs = append(d.Arcs, arcSpec{From: t.Name, To: p, Port: name, Transport: t.OutputTransports[name]})
		}
	}
	sort.Slice(d.Arcs, func(i, j int) bool { return d.Arcs[i].From+"\x00"+d.Arcs[i].To < d.Arcs[j].From+"\x00"+d.Arcs[j].To })
	for _, ref := range g.References {
		d.References = append(d.References, reference{Graph: ref.Graph, Namespace: ref.Namespace, Bindings: ref.Bindings})
	}
	return yaml.Marshal(d)
}
