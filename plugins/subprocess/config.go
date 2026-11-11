// Package subprocess binds local file-backed commands to finite Meshwork scenarios.
package subprocess

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config supplies execution details independently of the graph and scenarios.
type Config struct {
	Root            string             `yaml:"root"`
	Implementations map[string]Process `yaml:"implementations"`
	Inputs          map[string]string  `yaml:"inputs"`
	Outputs         map[string]string  `yaml:"outputs"`
}

// Process maps transition-local ports to paths inside a fresh working directory.
type Process struct {
	Argv    []string          `yaml:"argv"`
	Inputs  map[string]string `yaml:"inputs"`
	Outputs map[string]string `yaml:"outputs"`
}

// LoadConfig reads one YAML document and resolves its root against the config file.
func LoadConfig(path string) (Config, error) {
	var config Config
	file, err := os.Open(path)
	if err != nil {
		return config, err
	}
	defer file.Close()
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("subprocess config %s: %w", path, err)
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return Config{}, fmt.Errorf("subprocess config %s: expected one YAML document", path)
	}
	config.Root, err = filepath.Abs(resolvePath(filepath.Dir(path), config.Root))
	return config, err
}

func resolvePath(root, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(root, path)
}

func resolveCommand(root string, argv []string) ([]string, error) {
	if len(argv) == 0 || argv[0] == "" {
		return nil, fmt.Errorf("argv requires an executable")
	}
	command := make([]string, len(argv))
	for i, arg := range argv {
		command[i] = strings.ReplaceAll(arg, "${ROOT}", root)
	}
	if filepath.Base(command[0]) != command[0] {
		command[0] = resolvePath(root, command[0])
	}
	program, err := exec.LookPath(command[0])
	if err != nil {
		return nil, err
	}
	command[0], err = filepath.Abs(program)
	return command, err
}

func validatePorts(ports []string, files map[string]string) error {
	if len(ports) != len(files) {
		return fmt.Errorf("expected one file per port %v, got %d bindings", ports, len(files))
	}
	paths := map[string]bool{}
	for _, port := range ports {
		path := files[port]
		clean := filepath.Clean(path)
		if !filepath.IsLocal(path) || clean == "." || paths[clean] {
			return fmt.Errorf("invalid or duplicate staged path %q for port %s", path, port)
		}
		paths[clean] = true
	}
	return nil
}
