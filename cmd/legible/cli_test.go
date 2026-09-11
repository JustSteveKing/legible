package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

const (
	good     = "../../rules/testdata/good.yaml"
	petstore = "../../testdata/petstore.json"
)

// run drives the real command tree, returning stdout, stderr and the error
// main would turn into an exit code.
func run(t *testing.T, stdin string, args ...string) (string, string, error) {
	t.Helper()
	root := newRootCmd()
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), errOut.String(), err
}

func TestCheckGoodSpecExitsZero(t *testing.T) {
	out, stderr, err := run(t, "", "check", good)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "100  agent-ready") {
		t.Errorf("stdout does not report a perfect score:\n%s", out)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
}

// A gate failing (exit 1) and legible failing (exit 2) must stay apart:
// one means fix the spec, the other means fix the pipeline.
func TestExitCodesDistinguishGateFromFailure(t *testing.T) {
	for _, c := range []struct {
		name string
		args []string
		gate bool // want errGate
		fail bool // want some other error
	}{
		{"petstore has an error finding", []string{"check", petstore}, true, false},
		{"fail-on none", []string{"check", petstore, "--fail-on", "none"}, false, false},
		{"fail-under", []string{"check", good, "--fail-under", "100.1"}, true, false},
		{"fail-on info with nothing found", []string{"check", good, "--fail-on", "info"}, false, false},
		{"missing file", []string{"check", "nope.yaml"}, false, true},
		{"unknown rule", []string{"check", good, "--disable", "no-such-rule"}, false, true},
		{"bad fail-on", []string{"check", good, "--fail-on", "fatal"}, false, true},
		{"bad format", []string{"check", good, "-f", "xml"}, false, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := run(t, "", c.args...)
			isGate := errors.Is(err, errGate)
			if isGate != c.gate || (err != nil && !isGate) != c.fail {
				t.Errorf("err = %v; want gate=%v failure=%v", err, c.gate, c.fail)
			}
		})
	}
}

func TestDisableRemovesTheRule(t *testing.T) {
	out, _, _ := run(t, "", "check", petstore, "-f", "json", "--disable", "argument-collision")
	var rep struct {
		Rules []struct{ ID string }
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatal(err)
	}
	for _, r := range rep.Rules {
		if r.ID == "argument-collision" {
			t.Error("disabled rule still ran")
		}
	}
}

func TestMachineFormatsAreValid(t *testing.T) {
	out, _, _ := run(t, "", "check", petstore, "-f", "json", "--fail-on", "none")
	var rep struct {
		Score    float64
		Findings []struct{ Rule, Severity string }
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("json output does not parse: %v", err)
	}
	if rep.Score <= 0 || rep.Score >= 100 || len(rep.Findings) == 0 || rep.Findings[0].Severity != "error" {
		t.Errorf("json report = score %v, %d findings", rep.Score, len(rep.Findings))
	}

	out, _, _ = run(t, "", "check", petstore, "-f", "sarif", "--fail-on", "none")
	var sarif struct {
		Version string
		Runs    []struct {
			Tool struct {
				Driver struct{ Rules []struct{ ID string } }
			}
			Results []struct {
				RuleID    string
				Locations []struct {
					PhysicalLocation struct{ Region struct{ StartLine int } }
				}
			}
		}
	}
	if err := json.Unmarshal([]byte(out), &sarif); err != nil {
		t.Fatalf("sarif output does not parse: %v", err)
	}
	if sarif.Version != "2.1.0" || len(sarif.Runs) != 1 || len(sarif.Runs[0].Results) != len(rep.Findings) {
		t.Errorf("sarif: version %q, %d runs", sarif.Version, len(sarif.Runs))
	}
	for _, r := range sarif.Runs[0].Results {
		if r.Locations[0].PhysicalLocation.Region.StartLine < 1 {
			t.Errorf("%s: startLine must be positive", r.RuleID)
		}
	}

	out, _, _ = run(t, "", "check", petstore, "-f", "markdown", "--fail-on", "none")
	if !strings.HasPrefix(out, "## legible: ") {
		t.Errorf("markdown starts %q", out[:min(len(out), 40)])
	}
}

func TestReadsStdin(t *testing.T) {
	src, err := os.ReadFile(good)
	if err != nil {
		t.Fatal(err)
	}
	out, _, err := run(t, string(src), "check", "-")
	if err != nil || !strings.Contains(out, "<stdin>") {
		t.Errorf("err = %v, output:\n%s", err, out)
	}
}

func TestExplain(t *testing.T) {
	out, _, err := run(t, "", "explain", "tool-name")
	if err != nil || !strings.HasPrefix(out, "# tool-name") {
		t.Errorf("explain tool-name: err = %v, output starts %q", err, out[:min(len(out), 40)])
	}
	out, _, err = run(t, "", "explain")
	if err != nil || !strings.HasPrefix(out, "# How legible scores a spec") {
		t.Errorf("explain: err = %v", err)
	}
	if _, _, err := run(t, "", "explain", "no-such-rule"); err == nil {
		t.Error("explaining an unknown rule should fail")
	}
}

func TestRulesListsEveryRule(t *testing.T) {
	out, _, err := run(t, "", "rules")
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(out, "\n"); lines != 23 { // header + 22 rules
		t.Errorf("rules printed %d lines, want 23", lines)
	}
	if !strings.Contains(out, "max=15") {
		t.Error("rules does not show parameter-count's max option")
	}
}
