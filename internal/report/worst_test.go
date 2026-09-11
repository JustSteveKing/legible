package report

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/JustSteveKing/legible/engine"
	"github.com/JustSteveKing/legible/spec"
)

// ranked is a report whose operations rank, worst first: A (one error),
// B (5 warnings, 10 info), C (5 warnings, 2 info), D (50 info), with one
// document-level finding as well.
func ranked() *engine.Report {
	rule := func(id string, sev engine.Severity) *engine.Rule {
		return &engine.Rule{ID: id, Category: "x", Severity: sev}
	}
	e, w, i := rule("tool-name", engine.Error), rule("parameter-description", engine.Warning), rule("property-description", engine.Info)
	add := func(r *engine.Rule, op string, n int, into *engine.RuleResult) {
		for range n {
			into.Findings = append(into.Findings, engine.Finding{Rule: r.ID, Severity: r.Severity, Operation: op, Line: len(into.Findings) + 1, Message: op + " " + r.ID})
		}
		into.Failed, into.Checked = len(into.Findings), len(into.Findings)
	}
	er, wr, ir := engine.RuleResult{Rule: e}, engine.RuleResult{Rule: w}, engine.RuleResult{Rule: i}
	add(e, "GET /a", 1, &er)
	add(e, "", 1, &er) // a document-level finding
	add(w, "GET /b", 5, &wr)
	add(w, "GET /c", 5, &wr)
	add(i, "GET /b", 10, &ir)
	add(i, "GET /c", 2, &ir)
	add(i, "GET /d", 50, &ir)
	return &engine.Report{Document: "t.yaml", Version: "3.1.0", Operations: 4, Rules: []engine.RuleResult{er, wr, ir}}
}

func TestWorstFirst(t *testing.T) {
	var got []string
	for _, o := range worst(ranked().Findings()) {
		got = append(got, o.Operation)
	}
	if strings.Join(got, " ") != "GET /a GET /b GET /c GET /d" {
		t.Errorf("order = %v", got)
	}
}

func TestTextGroupsWorstFirst(t *testing.T) {
	var b strings.Builder
	if err := Text(&b, ranked(), Options{}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	order := []string{"document\n", "GET /a\n", "GET /b\n", "GET /c\n", "GET /d\n", "worst operations"}
	last := -1
	for _, s := range order {
		at := strings.Index(out, s)
		if at <= last {
			t.Fatalf("%q is out of order in:\n%s", s, out)
		}
		last = at
	}
	if !strings.Contains(out, "GET /b  5 warnings, 10 info") {
		t.Error("the worst-operations block does not count B's findings")
	}
}

func TestSummaryLeavesOutFindings(t *testing.T) {
	var b strings.Builder
	if err := Text(&b, ranked(), Options{Summary: true}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if strings.Contains(out, "GET /d property-description") {
		t.Error("--summary listed a finding")
	}
	for _, want := range []string{"worst operations", "findings by rule", "property-description", "--summary counts findings"} {
		if !strings.Contains(out, want) {
			t.Errorf("--summary lacks %q", want)
		}
	}
}

// Every format has to say when part of the spec was not checked.
func TestSkippedIsReportedEverywhere(t *testing.T) {
	rep := ranked()
	rep.Skipped = []spec.Skip{{URL: "https://example.com/common.yaml", Reason: "offline, and not in the cache"}}
	for format, want := range map[string][]string{
		"text":     {"(incomplete", "Not checked:", "https://example.com/common.yaml: offline"},
		"markdown": {"(incomplete)", "**Not checked:**", "`https://example.com/common.yaml`"},
		"sarif":    {`"toolExecutionNotifications"`, "Not checked: https://example.com/common.yaml"},
	} {
		var b strings.Builder
		if err := Write(&b, format, rep, Options{}); err != nil {
			t.Fatal(err)
		}
		for _, w := range want {
			if !strings.Contains(b.String(), w) {
				t.Errorf("%s lacks %q", format, w)
			}
		}
	}
	var b strings.Builder
	if err := JSON(&b, rep, Options{}); err != nil {
		t.Fatal(err)
	}
	var j struct {
		Complete bool
		Skipped  []spec.Skip
		Worst    []opCount `json:"worst_operations"`
	}
	if err := json.Unmarshal([]byte(b.String()), &j); err != nil {
		t.Fatal(err)
	}
	if j.Complete || len(j.Skipped) != 1 || len(j.Worst) != 4 || j.Worst[0].Operation != "GET /a" {
		t.Errorf("json: complete %v, skipped %v, worst %v", j.Complete, j.Skipped, j.Worst)
	}
}
