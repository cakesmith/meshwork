package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"example.com/meshwork-prototype/linker"
)

func runDiff(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("diff", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	format := flags.String("format", "text", "text or json")
	yed := flags.Bool("yed", false, "enable yEd shape/label conventions")
	var options, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			options = append(options, a)
			if (a == "--format" || a == "-format") && i+1 < len(args) {
				i++
				options = append(options, args[i])
			}
		} else {
			positional = append(positional, a)
		}
	}
	if err := flags.Parse(append(options, positional...)); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return fmt.Errorf("usage: meshwork diff BEFORE AFTER [--format text|json] [--yed]")
	}
	if *format != "text" && *format != "json" {
		return fmt.Errorf("unsupported diff format %q", *format)
	}
	before, err := linker.LoadComparison(flags.Arg(0), *yed)
	if err != nil {
		return fmt.Errorf("before: %w", err)
	}
	after, err := linker.LoadComparison(flags.Arg(1), *yed)
	if err != nil {
		return fmt.Errorf("after: %w", err)
	}
	report, err := linker.DiffResults(before, after)
	if err != nil {
		return err
	}
	if *format == "json" {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	return report.WriteText(out, flags.Arg(0), flags.Arg(1))
}
