package subprocess

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/cakesmith/meshwork/linker"
	"github.com/cakesmith/meshwork/meshwork"
)

type Artifact struct {
	SHA256    json.RawMessage `json:"sha256"`
	TokenHash string          `json:"token_hash"`
}

// Report identifies the captured and published files by canonical place name.
type Report struct {
	Scenario string              `json:"scenario"`
	Inputs   map[string]Artifact `json:"inputs"`
	Outputs  map[string]Artifact `json:"outputs"`
}

// Run snapshots the configured inputs, runs a linked scenario and publishes its files.
func (config Config) Run(ctx context.Context, graph meshwork.ExecutableGraph, scenario linker.Scenario, logs io.Writer) (Report, error) {
	if scenario.Input != "" || scenario.Output != "" || scenario.Clock.Type == "manual" {
		return Report{}, fmt.Errorf("subprocess execution requires a finite scenario with a real clock")
	}
	root, err := filepath.Abs(config.Root)
	if err != nil {
		return Report{}, err
	}
	workspace, err := os.MkdirTemp("", "meshwork-subprocess-")
	if err != nil {
		return Report{}, err
	}
	defer os.RemoveAll(workspace)
	store := artifactStore(workspace)
	types := make(map[string]string, len(graph.Places))
	for _, place := range graph.Places {
		types[place.Name] = place.Type
	}
	executors, err := config.bindExecutors(root, graph, types, store, logs)
	if err != nil {
		return Report{}, err
	}
	inputs, err := resolveFiles(root, scenario.Graph, types, config.Inputs)
	if err != nil {
		return Report{}, fmt.Errorf("inputs: %w", err)
	}
	outputs, err := resolveFiles(root, scenario.Graph, types, config.Outputs)
	if err != nil {
		return Report{}, fmt.Errorf("outputs: %w", err)
	}
	marking := make(map[string]meshwork.Token, len(scenario.Marking)+len(inputs))
	maps.Copy(marking, scenario.Marking)
	scenario.Marking = marking
	report := Report{Scenario: scenario.Name, Inputs: map[string]Artifact{}, Outputs: map[string]Artifact{}}
	for _, binding := range inputs {
		token, err := store.capture(binding.path, types[binding.place])
		if err != nil {
			return Report{}, fmt.Errorf("capture input %s: %w", binding.place, err)
		}
		if expected, ok := marking[binding.place]; ok && expected.Hash != token.Hash {
			return Report{}, fmt.Errorf("scenario %s requires input %s digest %s, got %s", scenario.Name, binding.place, expected.Data, token.Data)
		}
		marking[binding.place] = token
		report.Inputs[binding.place] = Artifact{SHA256: token.Data, TokenHash: token.Hash}
	}
	destinations := map[string]bool{}
	for _, binding := range outputs {
		if _, seeded := marking[binding.place]; seeded {
			return Report{}, fmt.Errorf("scenario %s must start with an empty output place %s", scenario.Name, binding.place)
		}
		if destinations[binding.path] {
			return Report{}, fmt.Errorf("duplicate output path %s", binding.path)
		}
		destinations[binding.path] = true
	}
	runtime, err := linker.RunScenarioWithRuntime(ctx, graph, scenario, func(clock meshwork.Clock) (*meshwork.Runtime, error) {
		return graph.InstantiateExecutors(executors, clock)
	})
	if err != nil {
		return Report{}, err
	}
	staged := make([]string, 0, len(outputs))
	for _, binding := range outputs {
		token, ok := runtime.Token(binding.place)
		if !ok {
			return Report{}, fmt.Errorf("scenario completed without output %s", binding.place)
		}
		temp, err := store.stage(token, binding.path)
		if err != nil {
			return Report{}, fmt.Errorf("stage output %s: %w", binding.place, err)
		}
		defer os.Remove(temp)
		staged = append(staged, temp)
		report.Outputs[binding.place] = Artifact{SHA256: token.Data, TokenHash: token.Hash}
	}
	for i, binding := range outputs {
		if err := os.Rename(staged[i], binding.path); err != nil {
			return Report{}, fmt.Errorf("publish output %s: %w", binding.place, err)
		}
	}
	return report, nil
}

type fileBinding struct {
	place, path string
}

func resolveFiles(root, namespace string, types, paths map[string]string) ([]fileBinding, error) {
	files := make([]fileBinding, 0, len(paths))
	seen := map[string]bool{}
	for _, name := range slices.Sorted(maps.Keys(paths)) {
		place, err := meshwork.Resolve(namespace, name)
		if err != nil {
			return nil, err
		}
		if types[place] == "" || seen[place] || paths[name] == "" {
			return nil, fmt.Errorf("invalid or duplicate file binding for %s", place)
		}
		seen[place] = true
		files = append(files, fileBinding{place: place, path: resolvePath(root, paths[name])})
	}
	return files, nil
}

func (config Config) bindExecutors(root string, graph meshwork.ExecutableGraph, types map[string]string, store artifactStore, logs io.Writer) (map[string]meshwork.Compute, error) {
	executors := make(map[string]meshwork.Compute, len(graph.Transitions))
	for _, transition := range graph.Transitions {
		process, ok := config.Implementations[transition.Implementation]
		if !ok {
			return nil, fmt.Errorf("%s: missing subprocess implementation %s", transition.Name, transition.Implementation)
		}
		for _, transports := range []map[string]string{transition.InputTransports, transition.OutputTransports} {
			for _, transport := range transports {
				if transport != "" && transport != "memory" {
					return nil, fmt.Errorf("%s: subprocess requires memory transport, got %s", transition.Name, transport)
				}
			}
		}
		if len(transition.Inputs) != len(transition.InputPorts) || len(transition.Outputs) != len(transition.OutputPorts) {
			return nil, fmt.Errorf("%s: invalid port mapping", transition.Name)
		}
		if err := validatePorts(transition.InputPorts, process.Inputs); err != nil {
			return nil, fmt.Errorf("%s inputs: %w", transition.Name, err)
		}
		if err := validatePorts(transition.OutputPorts, process.Outputs); err != nil {
			return nil, fmt.Errorf("%s outputs: %w", transition.Name, err)
		}
		argv, err := resolveCommand(root, process.Argv)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", transition.Name, err)
		}
		outputs := make(map[string]outputFile, len(transition.OutputPorts))
		for i, port := range transition.OutputPorts {
			outputs[port] = outputFile{path: process.Outputs[port], tokenType: types[transition.Outputs[i]]}
		}
		executors[transition.Name] = bindProcess(store, logs, argv, process.Inputs, outputs)
	}
	return executors, nil
}
