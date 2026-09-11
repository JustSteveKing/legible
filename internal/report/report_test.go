package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/JustSteveKing/legible/engine"
)

// many is a report with five findings of one rule in one operation, one of
// another rule there, and one of the first rule elsewhere.
func many() *engine.Report {
	pd := &engine.Rule{ID: "property-description", Category: "descriptions", Severity: engine.Info}
	tn := &engine.Rule{ID: "tool-name", Category: "naming", Severity: engine.Error}
	var pds []engine.Finding
	for i := 1; i <= 5; i++ {
		pds = append(pds, engine.Finding{Rule: pd.ID, Severity: engine.Info, Operation: "POST /a", Line: i, Message: fmt.Sprintf("nested-%d", i)})
	}
	pds = append(pds, engine.Finding{Rule: pd.ID, Severity: engine.Info, Operation: "GET /b", Line: 9, Message: "other-op"})
	return &engine.Report{
		Document: "t.yaml", Version: "3.1.0", Operations: 2,
		Rules: []engine.RuleResult{
			{Rule: pd, Checked: 6, Failed: 6, Findings: pds},
			{Rule: tn, Checked: 2, Failed: 1, Findings: []engine.Finding{{Rule: tn.ID, Severity: engine.Error, Operation: "POST /a", Line: 20, Message: "too-long"}}},
		},
	}
}

func render(t *testing.T, format string, opts Options) string {
	t.Helper()
	var b bytes.Buffer
	if err := Write(&b, format, many(), opts); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestTextCollapsesRepeatsWithinAnOperation(t *testing.T) {
	out := render(t, "text", Options{})
	for _, want := range []string{"nested-1", "nested-3", "… 2 more property-description", "too-long", "other-op", "2 of them not listed"} {
		if !strings.Contains(out, want) {
			t.Errorf("text output lacks %q", want)
		}
	}
	if strings.Contains(out, "nested-4") {
		t.Error("the fourth repeat was listed")
	}
	if v := render(t, "text", Options{Verbose: true}); !strings.Contains(v, "nested-5") || strings.Contains(v, "more property-description") {
		t.Error("--verbose did not list every finding")
	}
}

func TestMarkdownCollapsesToo(t *testing.T) {
	out := render(t, "markdown", Options{})
	if strings.Contains(out, "nested-4") || !strings.Contains(out, "| … 2 more | `property-description` |") {
		t.Errorf("markdown was not collapsed:\n%s", out)
	}
	if !strings.Contains(render(t, "markdown", Options{Verbose: true}), "nested-5") {
		t.Error("verbose markdown dropped a finding")
	}
}

// Markdown has to fit in a GitHub comment however big the spec is. Five
// hundred findings across as many operations must still produce at most
// maxMarkdownRows rows, and the per-rule table must still count all of them.
func TestMarkdownIsBounded(t *testing.T) {
	r := &engine.Rule{ID: "property-description", Category: "descriptions", Severity: engine.Info}
	var fs []engine.Finding
	for i := range 500 {
		fs = append(fs, engine.Finding{Rule: r.ID, Severity: engine.Info, Operation: fmt.Sprintf("GET /op%d", i), Line: i + 1, Message: "m"})
	}
	rep := &engine.Report{Document: "t.yaml", Version: "3.1.0", Rules: []engine.RuleResult{{Rule: r, Checked: 500, Failed: 500, Findings: fs}}}
	var b bytes.Buffer
	if err := Markdown(&b, rep, Options{}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if rows := strings.Count(out, "| info | GET /op"); rows != maxMarkdownRows {
		t.Errorf("%d finding rows, want %d", rows, maxMarkdownRows)
	}
	if !strings.Contains(out, "| `property-description` | info | 500 |") || !strings.Contains(out, "400 findings are counted above") {
		t.Errorf("the per-rule table or the note is missing:\n%s", out[:min(len(out), 600)])
	}
	if len(out) > 65536 {
		t.Errorf("%d bytes: too big for a GitHub comment", len(out))
	}
}

// Machine formats are for machines: they never collapse.
func TestJSONAndSARIFCarryEverything(t *testing.T) {
	var j struct{ Findings []struct{ Message string } }
	if err := json.Unmarshal([]byte(render(t, "json", Options{})), &j); err != nil || len(j.Findings) != 7 {
		t.Errorf("json: %d findings, err %v; want 7", len(j.Findings), err)
	}
	var s struct {
		Runs []struct{ Results []struct{} }
	}
	if err := json.Unmarshal([]byte(render(t, "sarif", Options{})), &s); err != nil || len(s.Runs[0].Results) != 7 {
		t.Errorf("sarif: err %v", err)
	}
}
