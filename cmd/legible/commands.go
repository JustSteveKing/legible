package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/JustSteveKing/legible/engine"
	"github.com/JustSteveKing/legible/internal/config"
	"github.com/JustSteveKing/legible/internal/report"
	"github.com/JustSteveKing/legible/rules"
	"github.com/JustSteveKing/legible/spec"
	"github.com/spf13/cobra"
)

type checkFlags struct {
	format    string
	failOn    string
	failUnder float64
	disable   []string
	only      []string
	color     string
	config    string
	noConfig  bool
	offline   bool
	noCache   bool
	refresh   bool
	verbose   bool
	summary   bool

	failOnSkipped bool
}

func newCheckCmd() *cobra.Command {
	f := &checkFlags{}
	cmd := &cobra.Command{
		Use:   "check <openapi.yaml|openapi.json|url|->",
		Short: "Check a spec and print its findings and score",
		Long: `Check a spec and print its findings and score.

Settings come from .legible.yaml, found in the working directory or the
nearest parent up to the repository root, or from --config. Flags override
the file. Run 'legible explain' for the file's format.`,
		Example: `  legible check openapi.yaml
  legible check openapi.json --format sarif > legible.sarif
  legible check openapi.yaml --fail-under 80 --fail-on warning
  legible check https://example.com/openapi.json --offline
  curl -s https://example.com/openapi.json | legible check -`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return runCheck(cmd, args[0], f) },
	}
	fl := cmd.Flags()
	fl.StringVarP(&f.format, "format", "f", "text", "output format: "+strings.Join(report.Formats, ", "))
	fl.StringVar(&f.failOn, "fail-on", "error", "exit 1 if any finding is at least this severe: error, warning, info or none")
	fl.Float64Var(&f.failUnder, "fail-under", 0, "exit 1 if the score is below this (0-100)")
	fl.BoolVar(&f.failOnSkipped, "fail-on-skipped", false, "exit 1 if any remote $ref could not be fetched, so part of the spec went unchecked")
	fl.StringSliceVar(&f.disable, "disable", nil, "rules to skip, comma separated")
	fl.StringSliceVar(&f.only, "only", nil, "run only these rules, comma separated")
	fl.StringVar(&f.color, "color", "auto", "colour text output: auto, always or never")
	fl.StringVar(&f.config, "config", "", "config file to use instead of searching for .legible.yaml")
	fl.BoolVar(&f.noConfig, "no-config", false, "ignore any .legible.yaml")
	fl.BoolVar(&f.offline, "offline", false, "do not fetch $refs to URLs; use cached copies where there are any")
	fl.BoolVar(&f.noCache, "no-cache", false, "neither read nor write the .legible cache of fetched $refs")
	fl.BoolVar(&f.refresh, "refresh", false, "revalidate every cached $ref now, however fresh its headers said it was")
	fl.BoolVar(&f.verbose, "verbose", false, "list every finding in text and markdown, rather than a few per rule per operation")
	fl.BoolVar(&f.summary, "summary", false, "scores, the worst operations and a count per rule, without the findings themselves")
	cmd.MarkFlagsMutuallyExclusive("verbose", "summary")
	cmd.MarkFlagsMutuallyExclusive("config", "no-config")
	_ = cmd.RegisterFlagCompletionFunc("format", cobra.FixedCompletions(report.Formats, cobra.ShellCompDirectiveNoFileComp))
	_ = cmd.RegisterFlagCompletionFunc("disable", completeRules)
	_ = cmd.RegisterFlagCompletionFunc("only", completeRules)
	return cmd
}

func runCheck(cmd *cobra.Command, path string, f *checkFlags) error {
	cfg, err := loadConfig(f)
	if err != nil {
		return err
	}

	// Flags beat the config file, which beats the defaults.
	failOn, failUnder, offline, failOnSkipped := "error", 0.0, false, false
	if cfg != nil {
		if cfg.FailOnSkipped != nil {
			failOnSkipped = *cfg.FailOnSkipped
		}
		if cfg.FailOn != "" {
			failOn = cfg.FailOn
		}
		if cfg.FailUnder != nil {
			failUnder = *cfg.FailUnder
		}
		if cfg.AllowRemote != nil {
			offline = !*cfg.AllowRemote
		}
	}
	flags := cmd.Flags()
	if flags.Changed("fail-on") {
		failOn = f.failOn
	}
	if flags.Changed("fail-under") {
		failUnder = f.failUnder
	}
	if flags.Changed("offline") {
		offline = f.offline
	}
	if flags.Changed("fail-on-skipped") {
		failOnSkipped = f.failOnSkipped
	}
	var threshold engine.Severity
	if failOn != "none" {
		if threshold = engine.ParseSeverity(failOn); threshold == 0 {
			return fmt.Errorf("--fail-on must be error, warning, info or none, not %q", failOn)
		}
	}

	ruleset := rules.All()
	if cfg != nil {
		if ruleset, err = cfg.Apply(ruleset); err != nil {
			return err
		}
	}
	selected, err := selectRules(ruleset, f.only, f.disable)
	if err != nil {
		return err
	}
	sopts := spec.Options{Offline: offline, Refresh: f.refresh}
	if !f.noCache {
		sopts.Cache = spec.DirCache{Dir: cacheDir(cfg)}
	}
	doc, err := load(cmd.InOrStdin(), path, sopts)
	if err != nil {
		return err
	}
	var opts engine.Options
	if cfg != nil {
		opts.Ignore = cfg.Ignores(doc.Path)
	}
	rep := engine.RunWith(doc, selected, opts)

	out := cmd.OutOrStdout()
	ropts := report.Options{Version: version, Color: useColor(f.color, out), Verbose: f.verbose, Summary: f.summary}
	if err := report.Write(out, f.format, rep, ropts); err != nil {
		return err
	}
	notices(cmd.ErrOrStderr(), rep, f.format, cfg, selected)

	// The other gates fail on findings the report shows. This one fails on
	// what the report could not cover, so it says why: someone reading only
	// the exit code would otherwise see a clean spec and a red build.
	if failOnSkipped && !rep.Complete() {
		fmt.Fprintf(cmd.ErrOrStderr(), "legible: fail-on-skipped: %d remote file(s) could not be fetched, so part of the spec was not checked\n", len(rep.Skipped))
		return errGate
	}
	if failUnder > 0 && rep.Score < failUnder {
		return errGate
	}
	if threshold > 0 {
		for _, finding := range rep.Findings() {
			if finding.Severity >= threshold {
				return errGate
			}
		}
	}
	return nil
}

// notices go to stderr, so they reach a person without corrupting a report
// someone is piping into a file. Text and Markdown already say what was not
// checked, so the coverage notices are only repeated for the machine
// formats, whose output a person usually never reads.
func notices(w io.Writer, rep *engine.Report, format string, cfg *config.Config, ran []*engine.Rule) {
	if format == "json" || format == "sarif" {
		for _, s := range rep.Skipped {
			fmt.Fprintf(w, "legible: not checked: %s could not be fetched (%s)\n", s.URL, s.Reason)
		}
		for _, u := range rep.Stale {
			fmt.Fprintf(w, "legible: could not fetch %s; checked the copy cached by an earlier run\n", u)
		}
	}
	if cfg == nil {
		return
	}
	for _, ig := range cfg.Unused(func(id string) bool {
		return slices.ContainsFunc(ran, func(r *engine.Rule) bool { return r.ID == id })
	}) {
		fmt.Fprintf(w, "legible: %s:%d: the ignore for %s matched nothing; if the problem is fixed, delete it\n",
			cfg.Path, ig.Line, ig.Rule)
	}
}

// cacheDir is where fetched $refs are kept between runs: beside the config
// file when there is one, since that marks the project; otherwise at the
// repository root; otherwise here. Nothing is created unless something is
// fetched.
func cacheDir(cfg *config.Config) string {
	if cfg != nil {
		return filepath.Join(filepath.Dir(cfg.Path), ".legible")
	}
	if root := config.RepoRoot("."); root != "" {
		return filepath.Join(root, ".legible")
	}
	return ".legible"
}

func loadConfig(f *checkFlags) (*config.Config, error) {
	if f.noConfig {
		return nil, nil
	}
	p := f.config
	if p == "" {
		var err error
		if p, err = config.Find("."); err != nil || p == "" {
			return nil, err
		}
	}
	return config.Load(p)
}

func load(stdin io.Reader, path string, opts spec.Options) (*spec.Document, error) {
	if path == "-" {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return nil, err
		}
		return spec.ParseWith("<stdin>", data, opts)
	}
	return spec.LoadWith(path, opts)
}

// selectRules applies --only and --disable to a ruleset, rejecting ids that
// do not exist: a typo in --disable that silently disables nothing is a
// check that quietly keeps failing, or worse, one somebody thinks is off.
func selectRules(ruleset []*engine.Rule, only, disable []string) ([]*engine.Rule, error) {
	for _, id := range append(slices.Clone(only), disable...) {
		if rules.Find(id) == nil {
			return nil, fmt.Errorf("unknown rule %q: run 'legible rules' for the list", id)
		}
	}
	var out []*engine.Rule
	for _, r := range ruleset {
		if len(only) > 0 && !slices.Contains(only, r.ID) {
			continue
		}
		if slices.Contains(disable, r.ID) {
			continue
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no rules left to run")
	}
	return out, nil
}

func useColor(mode string, w io.Writer) bool {
	switch mode {
	case "always":
		return true
	case "never":
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func newRulesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rules",
		Short: "List every rule with its category, severity and options",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "RULE\tCATEGORY\tSEVERITY\tOPTIONS\tCHECKS THAT")
			for _, r := range rules.All() {
				var opts []string
				for k, v := range r.Options {
					opts = append(opts, fmt.Sprintf("%s=%d", k, v))
				}
				slices.Sort(opts)
				o := strings.Join(opts, " ")
				if o == "" {
					o = "-"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.ID, r.Category, r.Severity, o, r.Title)
			}
			return tw.Flush()
		},
	}
}

func newExplainCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "explain [rule]",
		Short: "Explain why a rule matters and how to fix what it finds",
		Long:  "Print a rule's guide page. With no rule, print the overview: how legible scores a spec, and the config file.",
		Args:  cobra.MaximumNArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return completeRules(cmd, args, toComplete)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			id := "index"
			if len(args) == 1 {
				id = args[0]
				if rules.Find(id) == nil {
					return fmt.Errorf("unknown rule %q: run 'legible rules' for the list", id)
				}
			}
			_, err := io.WriteString(cmd.OutOrStdout(), rules.Guide(id))
			return err
		},
	}
}

func completeRules(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	var ids []string
	for _, r := range rules.All() {
		ids = append(ids, r.ID+"\t"+r.Title)
	}
	return ids, cobra.ShellCompDirectiveNoFileComp
}
