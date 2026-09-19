// Command prom-alert-linter lints Prometheus alerting rule files for quality
// problems beyond syntax: missing `for:`, missing severity label, absent
// summary/description/runbook annotations, constant expressions, and duplicate
// alert names.
//
// Exit codes:
//
//	0  no warnings (info-only findings still allowed)
//	1  at least one warning-level finding (CI gate)
//	2  runtime error (bad path, unreadable file, flags)
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/moveeeax/prom-alert-linter/internal/linter"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("prom-alert-linter", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "emit JSON instead of text")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "usage: prom-alert-linter [--json] FILE [FILE ...]\n\n")
		fmt.Fprintf(stderr, "Lints Prometheus alerting rule files for quality problems\n")
		fmt.Fprintf(stderr, "(missing for/severity/annotations, constant exprs, duplicate names).\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	paths := fs.Args()
	if len(paths) == 0 {
		fs.Usage()
		return 2
	}

	rep, err := linter.ScanFiles(paths)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}

	var werr error
	if *asJSON {
		werr = rep.WriteJSON(stdout)
	} else {
		werr = rep.WriteText(stdout)
	}
	if werr != nil {
		fmt.Fprintf(stderr, "error: %v\n", werr)
		return 2
	}

	if rep.WarnCount > 0 {
		return 1
	}
	return 0
}
