// Package linter checks Prometheus alerting rule files for quality problems
// that promtool's syntax check does not catch: alerts with no `for:`, a missing
// `severity` label, absent summary/description/runbook annotations, expressions
// that can never fire, and duplicate alert names.
//
// Files are parsed with yaml.v3 into a node tree rather than a struct so every
// finding carries the real source line number.
package linter

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ScanFiles lints the given rule files and merges the results. A file that
// cannot be read is a hard error. A file that is not valid YAML produces an
// invalid-yaml warning, so a broken rules file fails the CI gate instead of
// passing silently. Valid YAML without a `groups:` list is skipped (it is
// simply not an alerting-rules file).
func ScanFiles(paths []string) (*Report, error) {
	rep := &Report{}
	// Duplicate alert names are tracked across the whole run, since rules for
	// one alert are frequently split across files.
	type loc struct {
		file string
		line int
	}
	seen := map[string][]loc{}

	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		rep.FilesCount++
		for _, a := range lintFile(p, data, rep) {
			seen[a.name] = append(seen[a.name], loc{p, a.line})
		}
	}

	// Emit duplicate-name findings deterministically.
	var names []string
	for n, ls := range seen {
		if len(ls) > 1 {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		ls := seen[n]
		for _, l := range ls[1:] {
			rep.add(Finding{
				File: l.file, Line: l.line, Severity: SevWarn,
				Rule: RuleDuplicateName, Alert: n,
				Message: fmt.Sprintf("duplicate alert name %q (first defined at %s:%d)", n, ls[0].file, ls[0].line),
			})
		}
	}
	sortFindings(rep.Findings)
	return rep, nil
}

// alertRef notes an alert's name and its source line, used for cross-file
// duplicate detection.
type alertRef struct {
	name string
	line int
}

// lintFile parses one file and appends per-alert findings to rep, returning the
// alert references seen so the caller can detect duplicates across files.
func lintFile(file string, data []byte, rep *Report) []alertRef {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		rep.add(Finding{
			File: file, Line: errLine(err), Severity: SevWarn, Rule: RuleInvalidYAML,
			Message:    fmt.Sprintf("file is not valid YAML: %v", err),
			Suggestion: "fix the syntax; Prometheus will refuse to load this file",
		})
		return nil
	}
	if len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	groups := mapChild(root, "groups")
	if groups == nil || groups.Kind != yaml.SequenceNode {
		return nil // not a Prometheus rules document
	}

	var refs []alertRef
	for _, grp := range groups.Content {
		rules := mapChild(grp, "rules")
		if rules == nil || rules.Kind != yaml.SequenceNode {
			continue
		}
		for _, rule := range rules.Content {
			alertNode := mapChild(rule, "alert")
			if alertNode == nil {
				continue // a recording rule (`record:`), not an alert
			}
			name := alertNode.Value
			line := alertNode.Line
			refs = append(refs, alertRef{name, line})
			lintAlert(file, name, line, rule, rep)
		}
	}
	return refs
}

var yamlErrLine = regexp.MustCompile(`\bline (\d+):`)

// errLine extracts the 1-based line number from a yaml.v3 parse error such as
// "yaml: line 3: did not find expected key". It returns 1 when the error
// carries no line, so the finding still points at the file.
func errLine(err error) int {
	if m := yamlErrLine.FindStringSubmatch(err.Error()); m != nil {
		if n, convErr := strconv.Atoi(m[1]); convErr == nil && n > 0 {
			return n
		}
	}
	return 1
}

var constExpr = regexp.MustCompile(`^\s*(?:0|1|-?\d+(?:\.\d+)?)\s*$`)

// lintAlert applies the per-alert ruleset. `line` is the line of the `alert:`
// key, used as the anchor for whole-alert findings.
func lintAlert(file, name string, line int, rule *yaml.Node, rep *Report) {
	labels := mapChild(rule, "labels")
	annos := mapChild(rule, "annotations")

	if mapChild(rule, "for") == nil {
		rep.add(Finding{
			File: file, Line: line, Severity: SevWarn, Rule: RuleMissingFor, Alert: name,
			Message:    "alert has no `for:` — it fires on a single scrape and will flap",
			Suggestion: "add e.g. `for: 5m` so the condition must hold before firing",
		})
	}

	if labelValue(labels, "severity") == "" {
		rep.add(Finding{
			File: file, Line: line, Severity: SevWarn, Rule: RuleMissingSeverity, Alert: name,
			Message:    "alert has no `severity` label — Alertmanager routing usually keys on it",
			Suggestion: "add a `labels: { severity: warning }` (or critical/info)",
		})
	}

	if labelValue(annos, "summary") == "" {
		rep.add(Finding{
			File: file, Line: line, Severity: SevInfo, Rule: RuleMissingSummary, Alert: name,
			Message: "alert has no `summary` annotation",
		})
	}
	if labelValue(annos, "description") == "" {
		rep.add(Finding{
			File: file, Line: line, Severity: SevInfo, Rule: RuleMissingDescription, Alert: name,
			Message: "alert has no `description` annotation",
		})
	}
	if labelValue(annos, "runbook_url") == "" && labelValue(annos, "runbook") == "" {
		rep.add(Finding{
			File: file, Line: line, Severity: SevInfo, Rule: RuleMissingRunbook, Alert: name,
			Message: "alert has no `runbook_url` annotation — responders have nothing to open",
		})
	}

	if expr := mapChild(rule, "expr"); expr != nil && constExpr.MatchString(expr.Value) {
		v := strings.TrimSpace(expr.Value)
		msg := fmt.Sprintf("expression is the constant %s", v)
		if v == "0" {
			msg += " — this alert can never fire"
		} else {
			msg += " — this alert is always firing"
		}
		rep.add(Finding{
			File: file, Line: expr.Line, Severity: SevWarn, Rule: RuleConstantExpr, Alert: name,
			Message: msg,
		})
	}
}

// mapChild returns the value node for key within a mapping node, or nil.
func mapChild(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

// labelValue returns the scalar value of key inside a labels/annotations
// mapping node, or "" if the key is absent or blank.
func labelValue(m *yaml.Node, key string) string {
	v := mapChild(m, key)
	if v == nil {
		return ""
	}
	return strings.TrimSpace(v.Value)
}

// sortFindings orders findings by file, then line, then rule for stable output.
func sortFindings(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		if fs[i].File != fs[j].File {
			return fs[i].File < fs[j].File
		}
		if fs[i].Line != fs[j].Line {
			return fs[i].Line < fs[j].Line
		}
		return fs[i].Rule < fs[j].Rule
	})
}
