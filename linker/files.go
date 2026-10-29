// Package linker discovers and links distributed Meshwork definitions without
// loading or executing implementation source code.
package linker

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"example.com/meshwork-prototype/meshwork"
	"gopkg.in/yaml.v3"
)

type Repository struct {
	Name, Root, Namespace string
}

type File struct {
	Path, Kind, Name string
	Repository       Repository
}

var graphFile = regexp.MustCompile(`^graph\.([^.]+)\.meshwork\.yaml$`)
var scenarioFile = regexp.MustCompile(`^[^.]+\.(scenarios?)\.([^.]+)\.meshwork\.yaml$`)

// Discover returns sorted, absolute paths. Symlink directories are not followed.
func Discover(repos []Repository) ([]File, error) {
	var files []File
	seen := map[string]bool{}
	roots := map[string]string{}
	for _, repo := range repos {
		if repo.Name == "" || strings.ContainsAny(repo.Name, "/\\: .\t\n") || seen[repo.Name] {
			return nil, fmt.Errorf("invalid or duplicate repository name %q", repo.Name)
		}
		seen[repo.Name] = true
		root, err := filepath.Abs(repo.Root)
		if err != nil {
			return nil, err
		}
		repo.Root = root
		if old := roots[root]; old != "" {
			return nil, fmt.Errorf("repository root %s shared by %s and %s", root, old, repo.Name)
		}
		roots[root] = repo.Name
		if repo.Namespace == "" {
			repo.Namespace = "/"
		}
		if _, err := resolveName(repo.Namespace, repo.Name); err != nil {
			return nil, err
		}
		info, err := os.Stat(root)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("repository root is not a directory: %s", root)
		}
		err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if p != root && (d.Name() == ".git" || d.Name() == ".cache") {
					return filepath.SkipDir
				}
				return nil
			}
			kind, name := "", ""
			if m := graphFile.FindStringSubmatch(d.Name()); m != nil {
				kind, name = "graph", m[1]
			}
			if m := scenarioFile.FindStringSubmatch(d.Name()); m != nil {
				kind, name = m[1], m[2]
			}
			if kind != "" {
				if d.Type()&os.ModeSymlink != 0 {
					return fmt.Errorf("Meshwork source symlink is unsupported: %s", p)
				}
				files = append(files, File{p, kind, name, repo})
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

// duration accepts graph/scenario durations in Go notation, or explicit infinite.
type duration time.Duration

func parseDuration(s string) (time.Duration, error) {
	if s == "infinite" || s == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid nonnegative duration %q", s)
	}
	return d, nil
}
func (d *duration) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode || n.Tag != "!!str" || n.Value == "" {
		return fmt.Errorf("duration must be a string")
	}
	v, err := parseDuration(n.Value)
	*d = duration(v)
	return err
}
func (d duration) MarshalYAML() (any, error) { return time.Duration(d).String(), nil }

type transition struct {
	Implementation string   `yaml:"implementation"`
	Timeout        duration `yaml:"timeout,omitempty"`
}
type reference struct {
	Graph     string            `yaml:"graph"`
	Namespace string            `yaml:"namespace"`
	Bindings  map[string]string `yaml:"bindings"`
}
type clockSpec struct {
	Type  string        `yaml:"type"`
	Start time.Duration `yaml:"start"`
}
type scenarioStep struct {
	AdvanceClock *time.Duration `yaml:"advance_clock"`
	Expect       map[string]any `yaml:"expect"`
}
type scenario struct {
	Clock   clockSpec      `yaml:"clock"`
	Steps   []scenarioStep `yaml:"steps"`
	Name    string         `yaml:"name"`
	Marking map[string]any `yaml:"marking"`
	Expect  map[string]any `yaml:"expect"`
}
type document struct {
	Clock       clockSpec             `yaml:"clock"`
	Steps       []scenarioStep        `yaml:"steps"`
	Namespace   string                `yaml:"namespace"`
	Bindings    map[string]string     `yaml:"bindings"`
	Places      map[string]string     `yaml:"places"`
	Transitions map[string]transition `yaml:"transitions"`
	Arcs        [][]string            `yaml:"arcs"`
	References  []reference           `yaml:"references"`
	Graph       string                `yaml:"graph"`
	Name        string                `yaml:"name"`
	Marking     map[string]any        `yaml:"marking"`
	Expect      map[string]any        `yaml:"expect"`
	Scenarios   []scenario            `yaml:"scenarios"`
}
type source struct {
	File     File
	Doc      document
	Node     yaml.Node
	NS       string
	Imported *GraphMLDocument
}

func (s *source) loc(section, key string) string {
	if s.Imported != nil {
		for _, p := range s.Imported.Provenance {
			if p.Identity == key {
				return s.File.Path + "#" + p.ID
			}
		}
		return s.File.Path
	}
	n := &s.Node
	if len(n.Content) > 0 {
		n = n.Content[0]
	}
	line := n.Line
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value != section {
			continue
		}
		line = n.Content[i].Line
		v := n.Content[i+1]
		for j := 0; key != "" && j+1 < len(v.Content); j += 2 {
			if v.Content[j].Value == key {
				line = v.Content[j].Line
				break
			}
		}
	}
	return fmt.Sprintf("%s:%d", s.File.Path, line)
}

func parse(f File) (*source, error) {
	if f.Kind == "graphml" || f.Kind == "graphml-yed" {
		return parseGraphML(f)
	}
	if f.Kind != "graph" && f.Kind != "scenario" && f.Kind != "scenarios" {
		return nil, fmt.Errorf("%s: unsupported Meshwork file kind %q", f.Path, f.Kind)
	}
	b, err := os.ReadFile(f.Path)
	if err != nil {
		return nil, err
	}
	s := &source{File: f}
	d := yaml.NewDecoder(bytes.NewReader(b))
	d.KnownFields(true)
	if err = d.Decode(&s.Doc); err != nil {
		return nil, fmt.Errorf("%s: %w", f.Path, err)
	}
	var extra any
	if err = d.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("%s: expected one YAML document", f.Path)
	}
	if err = yaml.Unmarshal(b, &s.Node); err != nil {
		return nil, fmt.Errorf("%s: %w", f.Path, err)
	}
	// Restrict the shared decoding struct to the two conceptual file schemas.
	allowed := map[string]bool{}
	keys := "namespace bindings places transitions arcs references"
	if f.Kind != "graph" {
		keys = "namespace bindings graph name marking expect clock steps"
		if f.Kind == "scenarios" {
			keys = "namespace bindings graph scenarios clock"
		}
	}
	for _, k := range strings.Fields(keys) {
		allowed[k] = true
	}
	if len(s.Node.Content) == 0 || s.Node.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: expected a mapping", f.Path)
	}
	n := s.Node.Content[0]
	for i := 0; i < len(n.Content); i += 2 {
		if !allowed[n.Content[i].Value] {
			return nil, fmt.Errorf("%s:%d: field %q is invalid in %s file", f.Path, n.Content[i].Line, n.Content[i].Value, f.Kind)
		}
	}
	parent := f.Repository.Namespace
	if parent == "" {
		parent = "/"
	}
	s.NS, err = resolveName(parent, f.Repository.Name)
	if err == nil && s.Doc.Namespace != "" {
		s.NS, err = resolveName(s.NS, s.Doc.Namespace)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.Path, err)
	}
	if f.Kind == "graph" {
		if _, err := boundName(s, "/"); err != nil {
			return nil, fmt.Errorf("%s: %w", s.loc("bindings", ""), err)
		}
	}
	return s, nil
}

// Relative dot segments are supported here without changing kernel Resolve.
func resolveName(ns, name string) (string, error) {
	if ns != "/" {
		if _, err := meshwork.Resolve("/", ns); err != nil {
			return "", err
		}
	}
	if name == "" || !strings.HasPrefix(ns, "/") || strings.ContainsAny(name, "\\ \t\r\n") {
		return "", fmt.Errorf("invalid computational name %q in %q", name, ns)
	}
	for _, n := range []string{ns, name} {
		if strings.Contains(n, "//") || (n != "/" && strings.HasSuffix(n, "/")) {
			return "", fmt.Errorf("invalid computational name %q", n)
		}
	}
	parts := strings.Split(strings.TrimPrefix(ns, "/"), "/")
	if ns == "/" || strings.HasPrefix(name, "/") {
		parts = nil
	}
	for _, p := range strings.Split(strings.TrimPrefix(name, "/"), "/") {
		switch p {
		case ".":
		case "..":
			if len(parts) == 0 {
				return "", fmt.Errorf("name escapes computational root: %q", name)
			}
			parts = parts[:len(parts)-1]
		case "":
			if name != "/" {
				return "", fmt.Errorf("invalid computational name %q", name)
			}
		default:
			parts = append(parts, p)
		}
	}
	return "/" + strings.Join(parts, "/"), nil
}

func implementation(value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("implementation is required")
	}
	return value, nil
}

func port(name string) string { return path.Base(name) }
