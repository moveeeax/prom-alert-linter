package linter

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scan writes yml to a temp file and lints it, mirroring the CLI path.
func scan(t *testing.T, yml string) *Report {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "rules.yml")
	if err := os.WriteFile(p, []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err := ScanFiles([]string{p})
	if err != nil {
		t.Fatalf("ScanFiles: %v", err)
	}
	return rep
}

func findingsFor(rep *Report, rule Rule) []Finding {
	var out []Finding
	for _, f := range rep.Findings {
		if f.Rule == rule {
			out = append(out, f)
		}
	}
	return out
}

// TestAcceptance is the Notion acceptance criterion, verbatim: an alert without
// `for:` and without severity produces exactly two findings at the right line.
func TestAcceptance(t *testing.T) {
	yml := `
groups:
  - name: example
    rules:
      - alert: HighErrorRate
        expr: rate(errors[5m]) > 0.5
        labels:
          team: backend
        annotations:
          summary: too many errors
          description: error rate is high
          runbook_url: https://runbooks/errors
`
	rep := scan(t, yml)
	// The alert has summary/description/runbook and a labels block, so the only
	// gaps are `for` and `severity` — exactly two findings.
	if len(rep.Findings) != 2 {
		t.Fatalf("want exactly 2 findings, got %d: %+v", len(rep.Findings), rep.Findings)
	}
	rules := map[Rule]bool{}
	for _, f := range rep.Findings {
		rules[f.Rule] = true
		if f.Line != 5 { // the `- alert: HighErrorRate` line
			t.Fatalf("finding %s on line %d, want 5", f.Rule, f.Line)
		}
		if f.Severity != SevWarn {
			t.Fatalf("%s should be WARN, got %s", f.Rule, f.Severity)
		}
	}
	if !rules[RuleMissingFor] || !rules[RuleMissingSeverity] {
		t.Fatalf("want missing-for and missing-severity, got %+v", rep.Findings)
	}
}

func TestCleanAlertHasNoWarnings(t *testing.T) {
	yml := `
groups:
  - name: ok
    rules:
      - alert: DiskFilling
        expr: disk_free < 0.1
        for: 10m
        labels:
          severity: critical
        annotations:
          summary: disk almost full
          description: node {{ $labels.instance }} is nearly out of disk
          runbook_url: https://runbooks/disk
`
	rep := scan(t, yml)
	if rep.WarnCount != 0 || rep.InfoCount != 0 {
		t.Fatalf("a complete alert should be clean, got %+v", rep.Findings)
	}
}

func TestMissingAnnotationsAreInfo(t *testing.T) {
	yml := `
groups:
  - name: g
    rules:
      - alert: Bare
        expr: up == 0
        for: 1m
        labels:
          severity: warning
`
	rep := scan(t, yml)
	if rep.WarnCount != 0 {
		t.Fatalf("no warnings expected, got %+v", rep.Findings)
	}
	for _, want := range []Rule{RuleMissingSummary, RuleMissingDescription, RuleMissingRunbook} {
		if len(findingsFor(rep, want)) != 1 {
			t.Fatalf("want one %s finding, got %+v", want, rep.Findings)
		}
	}
	for _, f := range findingsFor(rep, RuleMissingSummary) {
		if f.Severity != SevInfo {
			t.Fatalf("missing annotations should be INFO, got %s", f.Severity)
		}
	}
}

func TestConstantExpression(t *testing.T) {
	yml := `
groups:
  - name: g
    rules:
      - alert: NeverFires
        expr: 0
        for: 5m
        labels: { severity: warning }
        annotations: { summary: x, description: y, runbook_url: z }
      - alert: AlwaysFires
        expr: 1
        for: 5m
        labels: { severity: warning }
        annotations: { summary: x, description: y, runbook_url: z }
`
	rep := scan(t, yml)
	cs := findingsFor(rep, RuleConstantExpr)
	if len(cs) != 2 {
		t.Fatalf("want 2 constant-expression findings, got %+v", rep.Findings)
	}
	var never, always bool
	for _, f := range cs {
		if strings.Contains(f.Message, "never fire") {
			never = true
		}
		if strings.Contains(f.Message, "always firing") {
			always = true
		}
	}
	if !never || !always {
		t.Fatalf("want both never-fire and always-firing messages, got %+v", cs)
	}
}

func TestDuplicateNamesAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	body := func(name string) string {
		return `
groups:
  - name: g
    rules:
      - alert: ` + name + `
        expr: up == 0
        for: 1m
        labels: { severity: warning }
        annotations: { summary: x, description: y, runbook_url: z }
`
	}
	a := filepath.Join(dir, "a.yml")
	b := filepath.Join(dir, "b.yml")
	os.WriteFile(a, []byte(body("Dup")), 0o600)
	os.WriteFile(b, []byte(body("Dup")), 0o600)

	rep, err := ScanFiles([]string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	dups := findingsFor(rep, RuleDuplicateName)
	if len(dups) != 1 {
		t.Fatalf("want exactly one duplicate finding (the second def), got %+v", rep.Findings)
	}
	if dups[0].Severity != SevWarn {
		t.Fatalf("duplicate-name should be WARN")
	}
}

func TestRecordingRulesIgnored(t *testing.T) {
	yml := `
groups:
  - name: rec
    rules:
      - record: job:rate
        expr: sum(rate(x[5m]))
`
	rep := scan(t, yml)
	if len(rep.Findings) != 0 {
		t.Fatalf("recording rules must not be linted as alerts, got %+v", rep.Findings)
	}
}

func TestNonRulesYAMLIgnored(t *testing.T) {
	rep := scan(t, "foo: bar\nbaz: [1,2,3]\n")
	if len(rep.Findings) != 0 {
		t.Fatalf("non-rules YAML should yield nothing, got %+v", rep.Findings)
	}
}

func TestMalformedYAMLSkipped(t *testing.T) {
	rep := scan(t, "groups: [this is: not valid: mapping")
	if len(rep.Findings) != 0 {
		t.Fatalf("unparseable YAML should be skipped, got %+v", rep.Findings)
	}
}

func TestLineNumberIsAlertLine(t *testing.T) {
	yml := "groups:\n  - name: g\n    rules:\n      - alert: X\n        expr: up == 0\n"
	rep := scan(t, yml)
	if len(rep.Findings) == 0 {
		t.Fatal("expected findings")
	}
	for _, f := range rep.Findings {
		if f.Rule == RuleMissingFor && f.Line != 4 {
			t.Fatalf("missing-for should anchor to the alert line 4, got %d", f.Line)
		}
	}
}

func TestReadErrorIsHardError(t *testing.T) {
	_, err := ScanFiles([]string{filepath.Join(t.TempDir(), "does-not-exist.yml")})
	if err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestJSONRoundTrip(t *testing.T) {
	yml := "groups:\n  - name: g\n    rules:\n      - alert: X\n        expr: up == 0\n"
	rep := scan(t, yml)
	var buf bytes.Buffer
	if err := rep.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	var back Report
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(back.Findings) != len(rep.Findings) {
		t.Fatalf("round trip lost findings: %+v", back)
	}
}

func TestTextOutput(t *testing.T) {
	yml := "groups:\n  - name: g\n    rules:\n      - alert: X\n        expr: up == 0\n"
	rep := scan(t, yml)
	var buf bytes.Buffer
	if err := rep.WriteText(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "missing-for") || !strings.Contains(out, "file(s) scanned") {
		t.Fatalf("text output missing expected content:\n%s", out)
	}
}
