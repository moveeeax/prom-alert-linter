# prom-alert-linter

[![ci](https://github.com/moveeeax/prom-alert-linter/actions/workflows/ci.yml/badge.svg)](https://github.com/moveeeax/prom-alert-linter/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/go-1.22%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Lint Prometheus **alerting rules** for quality problems that `promtool` never checks — and get a `file:line` for each one.

`promtool check rules` verifies syntax. It says nothing about alerts that flap because they have no `for:`, alerts that can't be routed because they have no `severity` label, alerts with no runbook, or expressions that can never fire. Those pass syntax and rot in production until a real incident gets lost in the noise. This tool makes them loud.

## What it catches

| Rule | Severity | What it means |
|------|----------|---------------|
| `missing-for`          | WARN | Alert has no `for:` — it fires on a single scrape and flaps |
| `missing-severity`     | WARN | No `severity` label — Alertmanager routing usually keys on it |
| `constant-expression`  | WARN | `expr` is a constant (`0` never fires, `1` always fires) |
| `duplicate-name`       | WARN | The same `alert:` name is defined more than once |
| `missing-summary`      | INFO | No `summary` annotation |
| `missing-description`  | INFO | No `description` annotation |
| `missing-runbook`      | INFO | No `runbook_url` annotation — responders have nothing to open |

Recording rules (`record:`) are ignored. A YAML file with no `groups:` mapping isn't a rules file and is skipped, not fatal.

## How it works

Each file is parsed with `yaml.v3` into a node tree rather than a struct, so every finding carries the real source line — a linter that says "something's wrong somewhere" gets ignored. Duplicate alert names are tracked across every file in one run, because rules for one alert are routinely split across files.

## Install

```sh
go install github.com/moveeeax/prom-alert-linter/cmd/prom-alert-linter@latest
```

Or build from source:

```sh
git clone https://github.com/moveeeax/prom-alert-linter.git
cd prom-alert-linter
go build ./cmd/prom-alert-linter
```

## Usage

```
usage: prom-alert-linter [--json] FILE [FILE ...]

  --json   emit JSON instead of text
```

```sh
$ prom-alert-linter examples/needs-work.yml
WARN  examples/needs-work.yml:5  [missing-for]  alert="HighRequestLatency"
  alert has no `for:` — it fires on a single scrape and will flap
  fix: add e.g. `for: 5m` so the condition must hold before firing
WARN  examples/needs-work.yml:5  [missing-severity]  alert="HighRequestLatency"
  alert has no `severity` label — Alertmanager routing usually keys on it
  fix: add a `labels: { severity: warning }` (or critical/info)
WARN  examples/needs-work.yml:12  [constant-expression]  alert="PlaceholderNeverFires"
  expression is the constant 0 — this alert can never fire
1 file(s) scanned — 3 warning(s), 5 info
```

Wire it into CI as a gate — it exits `1` on any warning:

```yaml
- run: go run github.com/moveeeax/prom-alert-linter/cmd/prom-alert-linter@latest rules/*.yml
```

## Exit codes

| Code | Meaning |
|------|---------|
| `0`  | No warnings (info-only findings allowed) |
| `1`  | At least one warning — CI gate |
| `2`  | Runtime error (bad path, unreadable file) |

## License

MIT — see [LICENSE](LICENSE).
