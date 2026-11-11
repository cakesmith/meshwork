package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"

	distributed "github.com/cakesmith/meshwork/examples/distributed"
	"github.com/cakesmith/meshwork/linker"
	"github.com/cakesmith/meshwork/plugins"
	"github.com/cakesmith/meshwork/plugins/remote"
	"github.com/cakesmith/meshwork/plugins/subprocess"
)

type repositories []linker.Repository

type roots []string

func (r *roots) String() string     { return strings.Join(*r, ",") }
func (r *roots) Set(v string) error { *r = append(*r, v); return nil }

func (r *repositories) String() string { return fmt.Sprint([]linker.Repository(*r)) }
func (r *repositories) Set(v string) error {
	name, root, ok := strings.Cut(v, "=")
	if !ok || name == "" || root == "" {
		return fmt.Errorf("repository must be NAME=PATH")
	}
	*r = append(*r, linker.Repository{Name: name, Root: root})
	return nil
}

func scenarioRepositoryRoot(scenario string) (string, error) {
	p, err := filepath.Abs(scenario)
	if err != nil {
		return "", err
	}
	for root := filepath.Dir(p); ; root = filepath.Dir(root) {
		if _, err := os.Stat(filepath.Join(root, "meshwork.repositories.yaml")); err == nil {
			return root, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(root)
		if parent == root {
			return "", fmt.Errorf("no repository manifest above scenario %s; provide a root or --repo", p)
		}
	}
}

func run(args []string, out io.Writer) error { return runInput(args, os.Stdin, out) }

func runInput(args []string, in io.Reader, out io.Writer) error {
	return runInputContext(context.Background(), args, in, out)
}

func runInputContext(ctx context.Context, args []string, in io.Reader, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: meshwork discover|link|validate|import [options] [ROOT|FILE ...]; meshwork execute --scenario FILE [options]; meshwork diff BEFORE AFTER [--format text|json] [--yed]")
	}
	command := args[0]
	if command == "diff" {
		return runDiff(args[1:], out)
	}
	if command != "discover" && command != "link" && command != "validate" && command != "import" && command != "execute" {
		return fmt.Errorf("unknown command %q", command)
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var repos repositories
	flags.Var(&repos, "repo", "repository NAME=PATH (repeatable)")
	ns := flags.String("namespace", "/", "parent computational namespace for repositories")
	var graphMLRoots roots
	flags.Var(&graphMLRoots, "graphml-root", "opt-in GraphML discovery root (repeatable)")
	yed := flags.Bool("yed", false, "infer computational kinds from yEd ellipse/rectangle and names from labels")
	format := flags.String("format", "", "canonical link/import output: json or yaml (import defaults to summary)")
	scenario := flags.String("scenario", "", "scenario source file to execute")
	deployment := flags.String("deployment", "", "executor deployment file for execute")
	processFile := flags.String("subprocess", "", "local subprocess bindings file for execute")
	// Permit flags after a filename as well as before it.
	var options, positional []string
	for i := 1; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			options = append(options, a)
			if i+1 < len(args) {
				switch strings.TrimLeft(a, "-") {
				case "repo", "namespace", "graphml-root", "format", "scenario", "deployment", "subprocess":
					i++
					options = append(options, args[i])
				}
			}
		} else {
			positional = append(positional, a)
		}
	}
	if err := flags.Parse(append(options, positional...)); err != nil {
		return err
	}
	if *format != "" && *format != "json" && *format != "yaml" {
		return fmt.Errorf("unsupported format %q", *format)
	}
	if command == "execute" && *scenario == "" {
		return fmt.Errorf("execute requires --scenario FILE")
	}
	if *deployment != "" && command != "execute" {
		return fmt.Errorf("--deployment is only valid for execute")
	}
	if *processFile != "" && (command != "execute" || *deployment != "") {
		return fmt.Errorf("--subprocess is only valid for execute without --deployment")
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	emit := func(result linker.Result) error {
		if command == "link" && len(result.Diagnostics) > 0 {
			return fmt.Errorf("graph is incomplete:\n%s", strings.Join(result.Diagnostics, "\n"))
		}
		if command == "import" && *format == "" {
			arcs := 0
			for _, t := range result.Graph.Transitions {
				arcs += len(t.Inputs) + len(t.Outputs)
			}
			var namespaces []string
			for n := range result.Definitions {
				if !strings.Contains(n, ":") {
					namespaces = append(namespaces, n)
				}
			}
			sort.Strings(namespaces)
			status := "Valid"
			if len(result.Diagnostics) > 0 {
				status = "Incomplete"
			}
			if _, err := fmt.Fprintf(out, "Imported: %s\nPlaces: %d\nTransitions: %d\nArcs: %d\nNamespace: %s\nStatus: %s\n", flags.Args()[0], len(result.Graph.Places), len(result.Graph.Transitions), arcs, strings.Join(namespaces, ", "), status); err != nil {
				return err
			}
			for _, d := range result.Diagnostics {
				if _, err := fmt.Fprintln(out, d); err != nil {
					return err
				}
			}
			return nil
		}
		if command == "import" || command == "link" {
			if *format == "yaml" {
				b, err := linker.CanonicalYAML(result.Graph)
				if err != nil {
					return err
				}
				_, err = out.Write(b)
				return err
			}
			if command == "link" {
				return encoder.Encode(result.Graph)
			}
			return encoder.Encode(result)
		}
		status := "valid"
		if len(result.Diagnostics) != 0 {
			status = "incomplete"
		}
		_, err := fmt.Fprintf(out, "%s: %d places, %d transitions, %d scenarios\n", status, len(result.Graph.Places), len(result.Graph.Transitions), len(result.Scenarios))
		if err != nil {
			return err
		}
		for _, d := range result.Diagnostics {
			if _, err := fmt.Fprintln(out, d); err != nil {
				return err
			}
		}
		if len(result.Diagnostics) != 0 {
			return fmt.Errorf("graph is incomplete")
		}
		return nil
	}
	if command == "import" || (command == "validate" && len(flags.Args()) == 1 && strings.EqualFold(filepath.Ext(flags.Args()[0]), ".graphml")) {
		if len(flags.Args()) != 1 || len(repos) != 0 || len(graphMLRoots) != 0 {
			return fmt.Errorf("%s requires exactly one GraphML file", command)
		}
		result, err := linker.ImportGraphML(flags.Args()[0], *yed)
		if err != nil {
			return err
		}
		return emit(result)
	}
	for _, root := range flags.Args() {
		absolute, err := filepath.Abs(root)
		if err != nil {
			return err
		}
		repos = append(repos, linker.Repository{Name: filepath.Base(absolute), Root: absolute})
	}
	if command == "execute" && len(repos) == 0 {
		root, err := scenarioRepositoryRoot(*scenario)
		if err != nil {
			return err
		}
		repos = append(repos, linker.Repository{Name: filepath.Base(root), Root: root})
	}
	if len(repos) == 0 {
		absolute, err := filepath.Abs(".")
		if err != nil {
			return err
		}
		repos = append(repos, linker.Repository{Name: filepath.Base(absolute), Root: absolute})
	}
	for i := range repos {
		repos[i].Namespace = *ns
	}
	files, err := linker.Discover(repos)
	if err != nil {
		return err
	}
	for _, root := range graphMLRoots {
		absolute, err := filepath.Abs(root)
		if err != nil {
			return err
		}
		graphRepo := linker.Repository{Name: filepath.Base(absolute), Root: absolute, Namespace: *ns}
		for _, r := range repos {
			configuredRoot, err := filepath.Abs(r.Root)
			if err != nil {
				return err
			}
			if configuredRoot == absolute {
				graphRepo = r
				break
			}
		}
		found, err := linker.DiscoverGraphML([]linker.Repository{graphRepo}, *yed)
		if err != nil {
			return err
		}
		files = append(files, found...)
	}
	if command == "discover" {
		return encoder.Encode(files)
	}
	result, err := linker.LinkFiles(files)
	if err != nil {
		return err
	}
	if command == "execute" {
		target, err := filepath.Abs(*scenario)
		if err != nil {
			return err
		}
		var localProcess subprocess.Config
		if *processFile != "" {
			localProcess, err = subprocess.LoadConfig(*processFile)
			if err != nil {
				return err
			}
		}
		matched := false
		for _, s := range result.Scenarios {
			if !strings.HasPrefix(s.Source, target+":") {
				continue
			}
			matched = true
			graph := result.Definitions[s.Graph]
			if *processFile != "" {
				report, err := localProcess.Run(ctx, graph, s, os.Stderr)
				if err != nil {
					return err
				}
				if err := encoder.Encode(report); err != nil {
					return err
				}
				continue
			}
			request := plugins.ExecutionRequest{Graph: graph, Scenario: s, Implementations: distributed.Implementations(), Input: in, Output: out}
			if err := func() error {
				if *deployment != "" {
					config, err := remote.LoadDeployment(*deployment)
					if err != nil {
						return err
					}
					authority, err := remote.Prepare(ctx, request.Graph, config, request.Implementations)
					if err != nil {
						return err
					}
					defer authority.Close()
					request.NewRuntime = authority.NewRuntime
				}
				return plugins.ExecuteScenario(ctx, request, plugins.BuiltIns()...)
			}(); err != nil {
				return err
			}
		}
		if !matched {
			return fmt.Errorf("scenario file was not discovered: %s", target)
		}
		return nil
	}
	return emit(result)
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := runInputContext(ctx, os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
