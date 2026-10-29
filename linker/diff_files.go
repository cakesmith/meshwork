package linker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadComparison independently resolves a file or workspace, permitting incomplete
// contracts. A stable repository name prevents directory names becoming diff noise.
func LoadComparison(filename string, yed bool) (Result, error) {
	absolute, err := filepath.Abs(filename)
	if err != nil {
		return Result{}, err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return Result{}, err
	}
	repo := Repository{Name: "workspace", Root: absolute, Namespace: "/"}
	var files []File
	if info.IsDir() {
		files, err = Discover([]Repository{repo})
		if err != nil {
			return Result{}, err
		}
		xml, err := DiscoverGraphML([]Repository{repo}, yed)
		if err != nil {
			return Result{}, err
		}
		files = append(files, xml...)
	} else {
		repo.Root = filepath.Dir(absolute)
		kind, name := "graph", "input"
		if strings.EqualFold(filepath.Ext(absolute), ".graphml") {
			kind = "graphml"
			if yed {
				kind = "graphml-yed"
			}
		} else if ext := strings.ToLower(filepath.Ext(absolute)); ext != ".yaml" && ext != ".yml" {
			return Result{}, fmt.Errorf("unsupported graph input %s", filename)
		}
		if m := graphFile.FindStringSubmatch(filepath.Base(absolute)); m != nil {
			name = m[1]
		}
		files = []File{{Path: absolute, Kind: kind, Name: name, Repository: repo}}
	}
	if len(files) == 0 {
		return Result{}, fmt.Errorf("no Meshwork graph sources found in %s", filename)
	}
	return linkFiles(files, true)
}
