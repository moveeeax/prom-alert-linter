package linter

// Severity classifies a finding.
type Severity string

const (
	SevWarn Severity = "WARN"
	SevInfo Severity = "INFO"
)

// Rule identifies which check produced a finding.
type Rule string

const (
	RuleMissingFor         Rule = "missing-for"
	RuleMissingSeverity    Rule = "missing-severity"
	RuleMissingSummary     Rule = "missing-summary"
	RuleMissingDescription Rule = "missing-description"
	RuleMissingRunbook     Rule = "missing-runbook"
	RuleDuplicateName      Rule = "duplicate-name"
	RuleConstantExpr       Rule = "constant-expression"
)

// Finding is a single lint result anchored to a file and line.
type Finding struct {
	File       string   `json:"file"`
	Line       int      `json:"line"`
	Severity   Severity `json:"severity"`
	Rule       Rule     `json:"rule"`
	Alert      string   `json:"alert"`
	Message    string   `json:"message"`
	Suggestion string   `json:"suggestion,omitempty"`
}

// Report aggregates findings across the scanned files.
type Report struct {
	Findings   []Finding `json:"findings"`
	FilesCount int       `json:"files_scanned"`
	WarnCount  int       `json:"warnings"`
	InfoCount  int       `json:"info"`
}

func (r *Report) add(f Finding) {
	r.Findings = append(r.Findings, f)
	switch f.Severity {
	case SevWarn:
		r.WarnCount++
	case SevInfo:
		r.InfoCount++
	}
}
