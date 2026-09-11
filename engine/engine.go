// Package engine runs rules over an OpenAPI document and scores the result.
//
// It knows nothing about any particular ruleset. A rule is a function over a
// spec.Document that records, through a Context, each place it looked and
// each place it found a problem. Recording the places it looked is what makes
// a score possible: a rule that checked 40 parameters and found 2 without a
// description is doing much better than one that checked 3 and found 2, and a
// bare count of findings cannot tell those apart.
package engine

import (
	"cmp"
	"fmt"
	"maps"
	"slices"

	"github.com/JustSteveKing/legible/spec"
	"go.yaml.in/yaml/v3"
)

// Severity grades how badly a finding hurts.
type Severity int

const (
	// Info is worth fixing but rarely stops an agent on its own.
	Info Severity = iota + 1
	// Warning makes an agent more likely to pick the wrong operation or send
	// the wrong arguments.
	Warning
	// Error stops an agent outright, or makes the operation impossible to
	// expose as a tool without hand editing.
	Error
)

func (s Severity) String() string {
	switch s {
	case Info:
		return "info"
	case Warning:
		return "warning"
	case Error:
		return "error"
	}
	return "unknown"
}

// ParseSeverity is the inverse of String. It returns 0 for anything else.
func ParseSeverity(s string) Severity {
	for _, v := range []Severity{Info, Warning, Error} {
		if v.String() == s {
			return v
		}
	}
	return 0
}

// weight is how much a rule of this severity counts towards its category's
// score. An unhelpful error body matters more than a missing example.
func (s Severity) weight() float64 { return float64(s) }

// Rule is one check.
type Rule struct {
	ID       string // kebab-case, stable: it is how people configure a rule
	Title    string
	Category string
	Severity Severity
	// Options are the rule's tunable thresholds and their current values.
	// The keys a rule declares here are the only ones configuration may set.
	Options map[string]int
	Check   func(*Context)
}

// Clone returns a copy that can be reconfigured without affecting r.
func (r *Rule) Clone() *Rule {
	c := *r
	c.Options = maps.Clone(r.Options)
	return &c
}

// Finding is one problem at one place.
type Finding struct {
	Rule     string   `json:"rule"`
	Severity Severity `json:"-"`
	Category string   `json:"category"`
	Message  string   `json:"message"`
	// Operation is the label of the operation the finding belongs to, or ""
	// for document-level findings.
	Operation string `json:"operation,omitempty"`
	// File is the file the finding is in, or "" for the root document.
	File    string `json:"file,omitempty"`
	Pointer string `json:"pointer"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
}

// Context is what a rule reports through.
type Context struct {
	Doc        *spec.Document
	rule       *Rule
	ignore     func(Finding) bool
	checked    int
	findings   []Finding
	suppressed []Finding
	// weight and failedWeight are checked and len(findings) with each place
	// counted at its weight. The score comes from these.
	weight, failedWeight float64
}

// Option returns the current value of one of the rule's declared options.
// Asking for one it did not declare is a bug in the rule, and panics.
func (c *Context) Option(name string) int {
	v, ok := c.rule.Options[name]
	if !ok {
		panic(fmt.Sprintf("rule %s reads option %q, which it does not declare", c.rule.ID, name))
	}
	return v
}

// Pass records a place the rule looked and found nothing wrong.
func (c *Context) Pass() {
	c.checked++
	c.weight++
}

// Fail records a place the rule looked and found a problem. node is used for
// the file, line and column; pass the most specific node available.
func (c *Context) Fail(op string, node *yaml.Node, ptr, msg string) {
	c.fail(1, op, node, ptr, msg)
}

// CheckWeighted is Check for a place that should count for more or less than
// one in the score. The finding is reported the same either way; only its
// share of the rule's score changes.
func (c *Context) CheckWeighted(ok bool, weight float64, op string, node *yaml.Node, ptr, msg string) {
	if ok {
		c.checked++
		c.weight += weight
		return
	}
	c.fail(weight, op, node, ptr, msg)
}

func (c *Context) fail(weight float64, op string, node *yaml.Node, ptr, msg string) {
	c.checked++
	c.weight += weight
	f := Finding{
		Rule:      c.rule.ID,
		Severity:  c.rule.Severity,
		Category:  c.rule.Category,
		Message:   msg,
		Operation: op,
		Pointer:   ptr,
	}
	if node != nil {
		f.Line, f.Column = node.Line, node.Column
		f.File = c.Doc.File(node)
	}
	if c.ignore != nil && c.ignore(f) {
		c.suppressed = append(c.suppressed, f)
		return
	}
	c.failedWeight += weight
	c.findings = append(c.findings, f)
}

// Check records a pass or a failure depending on ok.
func (c *Context) Check(ok bool, op string, node *yaml.Node, ptr, msg string) {
	if ok {
		c.Pass()
		return
	}
	c.Fail(op, node, ptr, msg)
}

// RuleResult is how one rule did.
type RuleResult struct {
	Rule    *Rule
	Checked int
	// Failed counts findings that were reported. Suppressed findings are
	// not in it: a finding someone has accepted counts as a pass.
	Failed     int
	Findings   []Finding
	Suppressed []Finding
	// Weight and FailedWeight are Checked and Failed with each place counted
	// at its weight, and are what the score is computed from. They equal the
	// counts unless the rule used CheckWeighted.
	Weight, FailedWeight float64
}

// Applicable reports whether the rule found anything to look at. A spec with
// no request bodies has nothing to say about request examples, and should
// neither gain nor lose for it.
func (r RuleResult) Applicable() bool { return r.Checked > 0 }

// Score is the weighted fraction of checked places that passed, from 0 to 1.
func (r RuleResult) Score() float64 {
	if r.Weight == 0 {
		return 1
	}
	return 1 - r.FailedWeight/r.Weight
}

// Report is the outcome of running a ruleset over one document.
type Report struct {
	Document   string
	Version    string
	Operations int
	Rules      []RuleResult
	Categories []CategoryScore
	// Score is the mean of the applicable category scores, from 0 to 100.
	Score float64
	// Skipped lists remote files that could not be fetched, so the schemas
	// behind them were not checked. A report with any is incomplete, and
	// its score describes only the part of the spec that was reached.
	Skipped []spec.Skip
	// Stale lists remote files checked from the cache because they could
	// not be fetched this run.
	Stale []string
}

// Complete reports whether every file the spec refers to was checked.
func (r *Report) Complete() bool { return len(r.Skipped) == 0 }

// CategoryScore is the severity-weighted mean of a category's applicable
// rule scores, from 0 to 100.
type CategoryScore struct {
	Name  string
	Score float64
	// Applicable is false when no rule in the category found anything to
	// check. Such a category is left out of the overall score.
	Applicable bool
}

// Findings returns every reported finding, most severe first, then in
// document order.
func (r *Report) Findings() []Finding {
	return sorted(r, func(rr RuleResult) []Finding { return rr.Findings })
}

// Suppressed returns the findings configuration chose to ignore, in the same
// order as Findings.
func (r *Report) Suppressed() []Finding {
	return sorted(r, func(rr RuleResult) []Finding { return rr.Suppressed })
}

func sorted(r *Report, pick func(RuleResult) []Finding) []Finding {
	var all []Finding
	for _, rr := range r.Rules {
		all = append(all, pick(rr)...)
	}
	slices.SortStableFunc(all, func(a, b Finding) int {
		return cmp.Or(
			cmp.Compare(b.Severity, a.Severity),
			cmp.Compare(a.File, b.File),
			cmp.Compare(a.Line, b.Line),
			cmp.Compare(a.Column, b.Column),
			cmp.Compare(a.Rule, b.Rule),
		)
	})
	return all
}

// Options adjusts a run.
type Options struct {
	// Ignore reports whether a finding has been accepted. An ignored finding
	// is kept in RuleResult.Suppressed and counts as a pass.
	Ignore func(Finding) bool
}

// Run applies rules to doc. Rules run in the order given, and categories are
// reported in the order they first appear in that list.
func Run(doc *spec.Document, rules []*Rule) *Report { return RunWith(doc, rules, Options{}) }

// RunWith is Run with options.
func RunWith(doc *spec.Document, rules []*Rule, opts Options) *Report {
	rep := &Report{Document: doc.Path, Version: doc.Version, Operations: len(doc.Operations())}
	type acc struct{ sum, weight float64 }
	cats := map[string]*acc{}
	var order []string
	for _, r := range rules {
		if _, ok := cats[r.Category]; !ok {
			cats[r.Category] = &acc{}
			order = append(order, r.Category)
		}
		ctx := &Context{Doc: doc, rule: r, ignore: opts.Ignore}
		r.Check(ctx)
		res := RuleResult{
			Rule: r, Checked: ctx.checked, Failed: len(ctx.findings),
			Findings: ctx.findings, Suppressed: ctx.suppressed,
			Weight: ctx.weight, FailedWeight: ctx.failedWeight,
		}
		rep.Rules = append(rep.Rules, res)
		if res.Applicable() {
			a := cats[r.Category]
			a.sum += res.Score() * r.Severity.weight()
			a.weight += r.Severity.weight()
		}
	}
	var total float64
	var n int
	for _, name := range order {
		a := cats[name]
		cs := CategoryScore{Name: name, Score: 100, Applicable: a.weight > 0}
		if cs.Applicable {
			cs.Score = 100 * a.sum / a.weight
			total += cs.Score
			n++
		}
		rep.Categories = append(rep.Categories, cs)
	}
	rep.Score = 100
	if n > 0 {
		rep.Score = total / float64(n)
	}
	// Files load as rules reach them, so these are only known now.
	rep.Skipped, rep.Stale = doc.Skipped(), doc.Stale()
	return rep
}
