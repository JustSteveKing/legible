// Package report renders an engine.Report for people and for machines.
package report

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"

	"github.com/JustSteveKing/legible/engine"
	"github.com/JustSteveKing/legible/spec"
)

// Formats lists the names Write accepts.
var Formats = []string{"text", "json", "markdown", "sarif"}

// Options controls rendering.
type Options struct {
	// Color enables ANSI colour in text output.
	Color bool
	// Version is legible's own version, recorded in machine formats.
	Version string
	// Verbose lists every finding in text and Markdown. Otherwise each
	// operation shows at most collapseAfter findings per rule.
	Verbose bool
	// Summary leaves the findings themselves out of text and Markdown,
	// keeping the scores, the worst operations and a count per rule: a
	// first look at a big spec.
	Summary bool
}

// collapseAfter is how many findings of one rule an operation shows, in the
// formats people read, before the rest become one line. Stripe's spec has
// operations with forty undocumented nested fields; listing them all buries
// the finding in the same operation that matters. JSON and SARIF always
// carry everything.
const collapseAfter = 3

// hidden is a count of findings left out by collapse.
type hidden struct {
	operation, rule string
	severity        engine.Severity
	n               int
}

// collapse keeps the first collapseAfter findings of each operation and rule,
// in order, and counts the rest.
func collapse(fs []engine.Finding, verbose bool) ([]engine.Finding, []hidden) {
	if verbose {
		return fs, nil
	}
	seen := map[[2]string]int{}
	at := map[[2]string]int{}
	var shown []engine.Finding
	var rest []hidden
	for _, f := range fs {
		k := [2]string{f.Operation, f.Rule}
		seen[k]++
		if seen[k] <= collapseAfter {
			shown = append(shown, f)
			continue
		}
		if i, ok := at[k]; ok {
			rest[i].n++
			continue
		}
		at[k] = len(rest)
		rest = append(rest, hidden{f.Operation, f.Rule, f.Severity, 1})
	}
	return shown, rest
}

func total(hs []hidden) int {
	n := 0
	for _, h := range hs {
		n += h.n
	}
	return n
}

// opCount is how many findings of each severity one operation has.
type opCount struct {
	Operation string `json:"operation"`
	Errors    int    `json:"errors"`
	Warnings  int    `json:"warnings"`
	Info      int    `json:"info"`
}

func (o opCount) String() string {
	var parts []string
	if o.Errors > 0 {
		parts = append(parts, fmt.Sprintf("%d error%s", o.Errors, plural(o.Errors)))
	}
	if o.Warnings > 0 {
		parts = append(parts, fmt.Sprintf("%d warning%s", o.Warnings, plural(o.Warnings)))
	}
	if o.Info > 0 {
		parts = append(parts, fmt.Sprintf("%d info", o.Info))
	}
	return strings.Join(parts, ", ")
}

// worst ranks the operations with findings, worst first: by errors, then
// warnings, then info. An error means an operation cannot be used as a tool
// at all, so one error outranks any number of warnings. Findings that belong
// to no operation are left out, since they describe no operation.
func worst(fs []engine.Finding) []opCount {
	idx := map[string]int{}
	var out []opCount
	for _, f := range fs {
		if f.Operation == "" {
			continue
		}
		i, ok := idx[f.Operation]
		if !ok {
			i = len(out)
			idx[f.Operation] = i
			out = append(out, opCount{Operation: f.Operation})
		}
		switch f.Severity {
		case engine.Error:
			out[i].Errors++
		case engine.Warning:
			out[i].Warnings++
		default:
			out[i].Info++
		}
	}
	slices.SortStableFunc(out, func(a, b opCount) int {
		return cmp.Or(
			cmp.Compare(b.Errors, a.Errors),
			cmp.Compare(b.Warnings, a.Warnings),
			cmp.Compare(b.Info, a.Info),
			strings.Compare(a.Operation, b.Operation),
		)
	})
	return out
}

// byRule returns the rules with findings, most severe first, then most
// findings first.
func byRule(rep *engine.Report) []engine.RuleResult {
	var out []engine.RuleResult
	for _, r := range rep.Rules {
		if r.Failed > 0 {
			out = append(out, r)
		}
	}
	slices.SortStableFunc(out, func(a, b engine.RuleResult) int {
		return cmp.Or(cmp.Compare(b.Rule.Severity, a.Rule.Severity), cmp.Compare(b.Failed, a.Failed))
	})
	return out
}

// Write renders rep in the named format.
func Write(w io.Writer, format string, rep *engine.Report, opts Options) error {
	switch format {
	case "text":
		return Text(w, rep, opts)
	case "json":
		return JSON(w, rep, opts)
	case "markdown":
		return Markdown(w, rep, opts)
	case "sarif":
		return SARIF(w, rep, opts)
	}
	return fmt.Errorf("unknown format %q: use one of %s", format, strings.Join(Formats, ", "))
}

const (
	reset  = "\x1b[0m"
	bold   = "\x1b[1m"
	dim    = "\x1b[2m"
	red    = "\x1b[31m"
	yellow = "\x1b[33m"
	blue   = "\x1b[34m"
	green  = "\x1b[32m"
)

// location is where a finding is: "line:column" in the root document, and
// "file:line:column" anywhere else.
func location(f engine.Finding) string {
	loc := fmt.Sprintf("%d:%d", f.Line, f.Column)
	if f.File != "" {
		loc = f.File + ":" + loc
	}
	return loc
}

// Text is the terminal format: findings grouped by operation, worst first,
// then scores, the worst operations again, and what was not checked.
func Text(w io.Writer, rep *engine.Report, opts Options) error {
	paint := func(code, s string) string {
		if !opts.Color {
			return s
		}
		return code + s + reset
	}
	sevColor := map[engine.Severity]string{engine.Error: red, engine.Warning: yellow, engine.Info: blue}

	findings := rep.Findings()
	ranked := worst(findings)
	groups := map[string][]engine.Finding{}
	width := 0
	for _, f := range findings {
		key := cmp.Or(f.Operation, "document")
		groups[key] = append(groups[key], f)
		width = max(width, len(location(f)))
	}
	// Findings that belong to no operation, such as a ref that does not
	// resolve, come first: they undermine everything after them.
	var order []string
	if _, ok := groups["document"]; ok {
		order = append(order, "document")
	}
	for _, o := range ranked {
		order = append(order, o.Operation)
	}

	notShown := 0
	if !opts.Summary {
		for _, key := range order {
			fmt.Fprintln(w, paint(bold, key))
			shown, rest := collapse(groups[key], opts.Verbose)
			for _, f := range shown {
				fmt.Fprintf(w, "  %s %s %s %s\n",
					paint(dim, fmt.Sprintf("%-*s", width, location(f))),
					paint(sevColor[f.Severity], fmt.Sprintf("%-7s", f.Severity)),
					f.Message,
					paint(dim, f.Rule))
			}
			for _, h := range rest {
				fmt.Fprintf(w, "  %s %s\n", strings.Repeat(" ", width+7), paint(dim, fmt.Sprintf("… %d more %s", h.n, h.rule)))
			}
			notShown += total(rest)
			fmt.Fprintln(w)
		}
	}

	fmt.Fprintf(w, "%s  %s, OpenAPI %s, %d operations\n\n", paint(bold, "legible"), rep.Document, rep.Version, rep.Operations)
	for _, c := range rep.Categories {
		if !c.Applicable {
			fmt.Fprintf(w, "  %-14s %s\n", c.Name, paint(dim, "  n/a  nothing to check"))
			continue
		}
		fmt.Fprintf(w, "  %-14s %5.0f  %s\n", c.Name, c.Score, bar(c.Score, paint))
	}
	g := grade(rep.Score)
	if !rep.Complete() {
		g += paint(yellow, " (incomplete: see below)")
	}
	fmt.Fprintf(w, "\n  %-14s %5.0f  %s\n", paint(bold, "score"), rep.Score, g)

	if len(ranked) > 0 {
		n := 5
		if opts.Summary {
			n = 10
		}
		top := ranked[:min(n, len(ranked))]
		lw := 0
		for _, o := range top {
			lw = max(lw, len(o.Operation))
		}
		fmt.Fprintf(w, "\n  %s\n", paint(bold, "worst operations"))
		for _, o := range top {
			fmt.Fprintf(w, "    %-*s  %s\n", lw, o.Operation, o)
		}
		if len(ranked) > n {
			fmt.Fprintf(w, "    %s\n", paint(dim, fmt.Sprintf("… and %d more with findings", len(ranked)-n)))
		}
	}
	if opts.Summary {
		if rules := byRule(rep); len(rules) > 0 {
			fmt.Fprintf(w, "\n  %s\n", paint(bold, "findings by rule"))
			for _, r := range rules {
				fmt.Fprintf(w, "    %-28s %-7s %6d\n", r.Rule.ID, r.Rule.Severity, r.Failed)
			}
		}
	}

	counts := map[engine.Severity]int{}
	for _, f := range findings {
		counts[f.Severity]++
	}
	ignored := ""
	if n := len(rep.Suppressed()); n > 0 {
		ignored = fmt.Sprintf(", %d ignored by config", n)
	}
	fmt.Fprintf(w, "\n  %d errors, %d warnings, %d info%s. `legible explain <rule>` says why a rule matters and how to fix it.\n",
		counts[engine.Error], counts[engine.Warning], counts[engine.Info], ignored)
	switch {
	case opts.Summary && len(findings) > 0:
		fmt.Fprintln(w, "  --summary counts findings without listing them; run without it to see each one.")
	case notShown > 0:
		fmt.Fprintf(w, "  %d of them not listed above, where one rule repeats within an operation; --verbose lists every one.\n", notShown)
	}

	if n := len(rep.Skipped); n > 0 {
		fmt.Fprintf(w, "\n  %s %d remote file%s could not be fetched, so the schemas behind %s were not checked:\n",
			paint(yellow, "Not checked:"), n, plural(n), them(n))
		for _, s := range rep.Skipped {
			fmt.Fprintf(w, "    %s: %s\n", s.URL, s.Reason)
		}
	}
	if len(rep.Stale) > 0 {
		fmt.Fprintf(w, "\n  %s could not be fetched this run, so the copies cached by an earlier run were checked:\n", paint(dim, "From cache:"))
		for _, u := range rep.Stale {
			fmt.Fprintf(w, "    %s\n", u)
		}
	}
	return nil
}

func bar(score float64, paint func(string, string) string) string {
	const width = 20
	full := int(math.Round(score / 100 * width))
	color := green
	switch {
	case score < 60:
		color = red
	case score < 85:
		color = yellow
	}
	return paint(color, strings.Repeat("█", full)) + paint(dim, strings.Repeat("░", width-full))
}

// grade turns a score into a word, so a number has a meaning attached.
func grade(score float64) string {
	switch {
	case score >= 90:
		return "agent-ready"
	case score >= 75:
		return "usable, with friction"
	case score >= 50:
		return "agents will struggle"
	}
	return "not agent-ready"
}

type jsonRule struct {
	ID       string         `json:"id"`
	Title    string         `json:"title"`
	Category string         `json:"category"`
	Severity string         `json:"severity"`
	Options  map[string]int `json:"options,omitempty"`
	Checked  int            `json:"checked"`
	Failed   int            `json:"failed"`
	Score    float64        `json:"score"`
}

type jsonFinding struct {
	engine.Finding
	Severity string `json:"severity"`
}

type jsonCategory struct {
	Name  string   `json:"name"`
	Score *float64 `json:"score"` // null when not applicable
}

// JSON is the machine format. Its shape is part of legible's interface:
// change it additively.
func JSON(w io.Writer, rep *engine.Report, opts Options) error {
	out := struct {
		Legible    string         `json:"legible"`
		Document   string         `json:"document"`
		OpenAPI    string         `json:"openapi"`
		Operations int            `json:"operations"`
		Score      float64        `json:"score"`
		Grade      string         `json:"grade"`
		Complete   bool           `json:"complete"`
		Categories []jsonCategory `json:"categories"`
		Rules      []jsonRule     `json:"rules"`
		Worst      []opCount      `json:"worst_operations"`
		Findings   []jsonFinding  `json:"findings"`
		Suppressed []jsonFinding  `json:"suppressed"`
		Skipped    []spec.Skip    `json:"skipped"`
		Stale      []string       `json:"stale"`
	}{
		Legible: opts.Version, Document: rep.Document, OpenAPI: rep.Version,
		Operations: rep.Operations, Score: round(rep.Score), Grade: grade(rep.Score), Complete: rep.Complete(),
		Categories: []jsonCategory{}, Rules: []jsonRule{}, Worst: []opCount{},
		Findings: []jsonFinding{}, Suppressed: []jsonFinding{}, Skipped: []spec.Skip{}, Stale: []string{},
	}
	for _, c := range rep.Categories {
		jc := jsonCategory{Name: c.Name}
		if c.Applicable {
			s := round(c.Score)
			jc.Score = &s
		}
		out.Categories = append(out.Categories, jc)
	}
	for _, r := range rep.Rules {
		out.Rules = append(out.Rules, jsonRule{r.Rule.ID, r.Rule.Title, r.Rule.Category, r.Rule.Severity.String(),
			r.Rule.Options, r.Checked, r.Failed, round(100 * r.Score())})
	}
	findings := rep.Findings()
	out.Worst = append(out.Worst, worst(findings)...)
	for _, f := range findings {
		out.Findings = append(out.Findings, jsonFinding{f, f.Severity.String()})
	}
	for _, f := range rep.Suppressed() {
		out.Suppressed = append(out.Suppressed, jsonFinding{f, f.Severity.String()})
	}
	out.Skipped = append(out.Skipped, rep.Skipped...)
	out.Stale = append(out.Stale, rep.Stale...)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func round(f float64) float64 { return math.Round(f*10) / 10 }

// maxMarkdownRows bounds the findings table in Markdown. GitHub caps a
// comment at 65,536 characters, and collapsing per operation is not enough on
// its own: Stripe's spec still came to 470 KB that way. A hundred rows of
// the most severe findings, under a per-rule table that counts everything,
// fits with room to spare.
const maxMarkdownRows = 100

// Markdown suits a pull request comment or a CI job summary. Its size is
// bounded unless Verbose is set: a per-rule table counts every finding, and
// at most maxMarkdownRows of them are listed.
func Markdown(w io.Writer, rep *engine.Report, opts Options) error {
	title := grade(rep.Score)
	if !rep.Complete() {
		title += " (incomplete)"
	}
	fmt.Fprintf(w, "## legible: %.0f/100, %s\n\n", rep.Score, title)
	fmt.Fprintf(w, "`%s`, OpenAPI %s, %d operations.", rep.Document, rep.Version, rep.Operations)
	if n := len(rep.Suppressed()); n > 0 {
		fmt.Fprintf(w, " %d finding%s ignored by config.", n, plural(n))
	}
	fmt.Fprint(w, "\n\n")
	if n := len(rep.Skipped); n > 0 {
		fmt.Fprintf(w, "> **Not checked:** %d remote file%s could not be fetched, so the schemas behind %s were not checked.\n>\n", n, plural(n), them(n))
		for _, s := range rep.Skipped {
			fmt.Fprintf(w, "> - `%s`: %s\n", s.URL, cell(s.Reason))
		}
		fmt.Fprintln(w)
	}
	if len(rep.Stale) > 0 {
		fmt.Fprintf(w, "> Checked from the cache, because they could not be fetched this run: %s\n\n", "`"+strings.Join(rep.Stale, "`, `")+"`")
	}
	fmt.Fprintln(w, "| Category | Score |")
	fmt.Fprintln(w, "|---|---:|")
	for _, c := range rep.Categories {
		if c.Applicable {
			fmt.Fprintf(w, "| %s | %.0f |\n", c.Name, c.Score)
		} else {
			fmt.Fprintf(w, "| %s | n/a |\n", c.Name)
		}
	}
	findings := rep.Findings()
	if len(findings) == 0 {
		fmt.Fprintln(w, "\nNo findings.")
		return nil
	}

	// Every finding is counted here, whatever the table below leaves out.
	fmt.Fprintln(w, "\n| Rule | Severity | Findings |")
	fmt.Fprintln(w, "|---|---|---:|")
	for _, r := range byRule(rep) {
		fmt.Fprintf(w, "| `%s` | %s | %d |\n", r.Rule.ID, r.Rule.Severity, r.Failed)
	}

	if ranked := worst(findings); len(ranked) > 0 {
		fmt.Fprintln(w, "\n| Worst operations | Errors | Warnings | Info |")
		fmt.Fprintln(w, "|---|---:|---:|---:|")
		for _, o := range ranked[:min(10, len(ranked))] {
			fmt.Fprintf(w, "| %s | %d | %d | %d |\n", cell(o.Operation), o.Errors, o.Warnings, o.Info)
		}
	}
	if opts.Summary {
		fmt.Fprintln(w, "\nFindings are counted, not listed, under `--summary`.")
		return nil
	}

	fmt.Fprintf(w, "\n<details><summary>%d findings</summary>\n\n", len(findings))
	fmt.Fprintln(w, "| Severity | Where | Finding | Rule |")
	fmt.Fprintln(w, "|---|---|---|---|")
	where := func(op string) string { return cmp.Or(op, "document") }
	shown, rest := collapse(findings, opts.Verbose)
	listed, rows := 0, 0
	for _, f := range shown {
		if !opts.Verbose && rows == maxMarkdownRows {
			break
		}
		fmt.Fprintf(w, "| %s | %s (%s) | %s | `%s` |\n", f.Severity, cell(where(f.Operation)), location(f), cell(f.Message), f.Rule)
		listed++
		rows++
	}
	for _, h := range rest {
		if rows == maxMarkdownRows {
			break
		}
		fmt.Fprintf(w, "| %s | %s | … %d more | `%s` |\n", h.severity, cell(where(h.operation)), h.n, h.rule)
		rows++
	}
	if n := len(findings) - listed; n > 0 {
		fmt.Fprintf(w, "\n%d findings are counted above rather than listed. `--verbose` lists them all; `-f json` and `-f sarif` always carry every one.\n", n)
	}
	fmt.Fprintln(w, "\n</details>")
	return nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func them(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}

func cell(s string) string { return strings.NewReplacer("|", `\|`, "\n", " ").Replace(s) }
