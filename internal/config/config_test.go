package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JustSteveKing/legible/engine"
)

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func load(t *testing.T, body string) (*Config, error) {
	t.Helper()
	return Load(write(t, t.TempDir(), ".legible.yaml", body))
}

// rules stands in for the real ruleset, so these tests do not depend on it.
func rules() []*engine.Rule {
	return []*engine.Rule{
		{ID: "parameter-count", Severity: engine.Warning, Options: map[string]int{"max": 15}},
		{ID: "property-description", Severity: engine.Info},
		{ID: "error-shape", Severity: engine.Info},
	}
}

const example = `
fail-on: warning
fail-under: 75
allow-remote: false

rules:
  property-description: off
  error-shape: error
  parameter-count:
    severity: info
    max: 30

ignore:
  - rule: parameter-count
    operations:
      - GET /v1/transactions
      - GET /v1/stats/*
    reason: Wide search endpoints; every filter is a real one.
`

func TestTheDocumentedExample(t *testing.T) {
	c, err := load(t, example)
	if err != nil {
		t.Fatal(err)
	}
	if c.FailOn != "warning" || *c.FailUnder != 75 || *c.AllowRemote {
		t.Errorf("gate = %q %v %v", c.FailOn, *c.FailUnder, *c.AllowRemote)
	}
	in := rules()
	out, err := c.Apply(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d rules, want 2 with property-description off", len(out))
	}
	pc, es := out[0], out[1]
	if pc.Severity != engine.Info || pc.Options["max"] != 30 || es.Severity != engine.Error {
		t.Errorf("parameter-count %v max=%d, error-shape %v", pc.Severity, pc.Options["max"], es.Severity)
	}
	if in[0].Severity != engine.Warning || in[0].Options["max"] != 15 {
		t.Error("Apply modified the rules it was given")
	}
}

func TestRejects(t *testing.T) {
	for name, c := range map[string]struct{ body, want string }{
		"misspelt key":       {"fail-undr: 80\n", "field fail-undr not found"},
		"bad fail-on":        {"fail-on: fatal\n", "fail-on must be"},
		"fail-under range":   {"fail-under: 101\n", "between 0 and 100"},
		"bad severity":       {"rules: {error-shape: fatal}\n", `not "fatal"`},
		"non-numeric option": {"rules: {parameter-count: {max: lots}}\n", "whole number"},
		"reasonless ignore":  {"ignore: [{rule: error-shape, operations: [GET /a]}]\n", "needs a reason"},
		"ignore everywhere":  {"ignore: [{rule: error-shape, reason: x}]\n", "set it to off"},
		"bad pattern":        {"ignore: [{rule: error-shape, operations: ['GET /[a'], reason: x}]\n", "bad operation pattern"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := load(t, c.body)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want it to mention %q", err, c.want)
			}
		})
	}
}

func TestApplyRejectsNamesThatDoNotExist(t *testing.T) {
	for name, c := range map[string]struct{ body, want string }{
		"unknown rule":   {"rules: {no-such-rule: off}\n", `unknown rule "no-such-rule"`},
		"unknown option": {"rules: {parameter-count: {maximum: 3}}\n", `no option "maximum"; it has max`},
		"optionless":     {"rules: {error-shape: {max: 3}}\n", "it has none"},
		"ignored rule":   {"ignore: [{rule: nope, operations: [GET /a], reason: x}]\n", `unknown rule "nope"`},
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := load(t, c.body)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := cfg.Apply(rules()); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want it to mention %q", err, c.want)
			}
		})
	}
}

func TestIgnores(t *testing.T) {
	c, err := load(t, `
ignore:
  - {rule: parameter-count, operations: ["GET /v1/stats/*"], reason: wide}
  - {rule: parameter-count, operations: ["* /admin/*"], reason: internal}
  - {rule: error-shape, operations: ["GET /never"], reason: stale}
`)
	if err != nil {
		t.Fatal(err)
	}
	ignore := c.Ignores("openapi.yaml")
	for op, want := range map[string]bool{
		"GET /v1/stats/summary":  true,
		"GET /v1/stats/a/b":      false, // * stays within a segment
		"POST /admin/users":      true,
		"GET /v1/transactions":   false,
		"DELETE /admin/sessions": true,
	} {
		if got := ignore(engine.Finding{Rule: "parameter-count", Operation: op}); got != want {
			t.Errorf("%s: ignored = %v, want %v", op, got, want)
		}
	}
	unused := c.Unused(func(string) bool { return true })
	if len(unused) != 1 || unused[0].Rule != "error-shape" || unused[0].Line == 0 {
		t.Errorf("unused = %+v", unused)
	}
	if len(c.Unused(func(id string) bool { return id != "error-shape" })) != 0 {
		t.Error("an ignore for a rule that did not run cannot be judged stale")
	}
}

func TestFindStopsAtTheRepository(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	write(t, root, ".legible.yaml", "{}\n") // above the repository: not ours
	write(t, repo, ".git/HEAD", "")
	deep := filepath.Join(repo, "api", "v1")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if p, err := Find(deep); err != nil || p != "" {
		t.Errorf("Find = %q, %v; want nothing, since the only config is above .git", p, err)
	}
	want := write(t, repo, ".legible.yml", "{}\n")
	if p, _ := Find(deep); p != want {
		t.Errorf("Find = %q, want %q", p, want)
	}
}

func TestFailOnSkipped(t *testing.T) {
	c, err := load(t, "fail-on-skipped: true\n")
	if err != nil || c.FailOnSkipped == nil || !*c.FailOnSkipped {
		t.Errorf("fail-on-skipped = %v, err %v", c.FailOnSkipped, err)
	}
	if c, _ := load(t, "{}\n"); c.FailOnSkipped != nil {
		t.Error("unset should be nil, so the flag's default applies")
	}
}

func TestEmptyFileIsValid(t *testing.T) {
	if _, err := load(t, ""); err != nil {
		t.Errorf("an empty config should be valid: %v", err)
	}
}
