package engine

import (
	"math"
	"testing"

	"github.com/JustSteveKing/legible/spec"
	"go.yaml.in/yaml/v3"
)

func doc(t *testing.T) *spec.Document {
	t.Helper()
	d, err := spec.Parse("t.yaml", []byte("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// fake records passes and failures in the given order, failing at the line
// numbers listed in fails.
func fake(id, cat string, sev Severity, passes int, fails ...int) *Rule {
	return &Rule{ID: id, Category: cat, Severity: sev, Check: func(c *Context) {
		for range passes {
			c.Pass()
		}
		for _, line := range fails {
			c.Fail("", &yaml.Node{Line: line, Column: 1}, "", id)
		}
	}}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// The arithmetic the guide promises, worked by hand:
//
//	a: error,   3 of 4 pass → 0.75, weight 3
//	b: info,    0 of 2 pass → 0,    weight 1
//	x = (0.75×3 + 0×1) / 4 = 56.25
//	c: warning, nothing to check → y is n/a and left out
//	d: warning, 1 of 1 → z = 100
//	overall = (56.25 + 100) / 2 = 78.125
func TestScoring(t *testing.T) {
	rep := Run(doc(t), []*Rule{
		fake("a", "x", Error, 3, 10),
		fake("b", "x", Info, 0, 20, 30),
		fake("c", "y", Warning, 0),
		fake("d", "z", Warning, 1),
	})
	want := []CategoryScore{{"x", 56.25, true}, {"y", 100, false}, {"z", 100, true}}
	if len(rep.Categories) != len(want) {
		t.Fatalf("categories = %+v", rep.Categories)
	}
	for i, w := range want {
		g := rep.Categories[i]
		if g.Name != w.Name || g.Applicable != w.Applicable || !near(g.Score, w.Score) {
			t.Errorf("category %d = %+v, want %+v", i, g, w)
		}
	}
	if !near(rep.Score, 78.125) {
		t.Errorf("score = %v, want 78.125", rep.Score)
	}
}

// A weighted place changes the score, not the counts: one pass at weight 1
// and one failure at 0.25 is 1 − 0.25/1.25 = 0.8, where unweighted it would
// be 0.5. A suppressed failure counts as a pass at its weight.
func TestWeightedChecks(t *testing.T) {
	r := &Rule{ID: "w", Category: "x", Severity: Info, Check: func(c *Context) {
		c.CheckWeighted(true, 1, "", nil, "", "")
		c.CheckWeighted(false, 0.25, "", &yaml.Node{Line: 1}, "", "small")
		c.CheckWeighted(false, 1, "GET /ignored", &yaml.Node{Line: 2}, "", "big")
	}}
	rep := RunWith(doc(t), []*Rule{r}, Options{Ignore: func(f Finding) bool { return f.Operation == "GET /ignored" }})
	res := rep.Rules[0]
	if res.Checked != 3 || res.Failed != 1 || len(res.Suppressed) != 1 {
		t.Errorf("checked %d failed %d suppressed %d", res.Checked, res.Failed, len(res.Suppressed))
	}
	// passes: 1 + 1 (suppressed) ; failed: 0.25 ; total 2.25
	if want := 1 - 0.25/2.25; !near(res.Score(), want) {
		t.Errorf("score = %v, want %v", res.Score(), want)
	}
}

func TestNothingApplicableScoresFull(t *testing.T) {
	rep := Run(doc(t), []*Rule{fake("c", "y", Warning, 0)})
	if rep.Score != 100 {
		t.Errorf("score = %v, want 100 when no rule had anything to check", rep.Score)
	}
}

func TestFindingsAreMostSevereFirstThenInDocumentOrder(t *testing.T) {
	rep := Run(doc(t), []*Rule{
		fake("info", "x", Info, 0, 1),
		fake("error", "x", Error, 0, 9, 5),
		fake("warning", "x", Warning, 0, 2),
	})
	var got []int
	for _, f := range rep.Findings() {
		got = append(got, f.Line)
	}
	want := []int{5, 9, 2, 1}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("lines in order = %v, want %v", got, want)
		}
	}
}

func TestParseSeverity(t *testing.T) {
	for _, s := range []Severity{Info, Warning, Error} {
		if ParseSeverity(s.String()) != s {
			t.Errorf("round trip of %v failed", s)
		}
	}
	if ParseSeverity("fatal") != 0 {
		t.Error("an unknown severity should parse as 0")
	}
}
