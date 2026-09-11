// Package config reads .legible.yaml, which records how a project runs
// legible: the gate, which rules apply and how strictly, and which findings
// have been accepted and why.
package config

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/JustSteveKing/legible/engine"
	"go.yaml.in/yaml/v3"
)

// Names are the file names Find looks for, in order.
var Names = []string{".legible.yaml", ".legible.yml"}

// Config is a parsed .legible.yaml. Pointer fields are nil when the file
// does not set them, so flags and defaults can tell "unset" from "zero".
type Config struct {
	Path        string   `yaml:"-"`
	FailOn      string   `yaml:"fail-on"`
	FailUnder   *float64 `yaml:"fail-under"`
	AllowRemote *bool    `yaml:"allow-remote"`
	// FailOnSkipped fails the gate when a remote file could not be fetched.
	// With allow-remote: false it means every remote file must be cached.
	FailOnSkipped *bool                 `yaml:"fail-on-skipped"`
	Rules         map[string]RuleConfig `yaml:"rules"`
	Ignore        []Ignore              `yaml:"ignore"`
}

// RuleConfig is one entry under rules. In YAML it is either a scalar,
// `off` or a severity, or a mapping of `severity` and the rule's options.
type RuleConfig struct {
	Off      bool
	Severity engine.Severity // 0 leaves the rule's own severity
	Options  map[string]int
	Line     int
}

// UnmarshalYAML accepts the two shapes RuleConfig documents.
func (r *RuleConfig) UnmarshalYAML(n *yaml.Node) error {
	r.Line = n.Line
	switch n.Kind {
	case yaml.ScalarNode:
		return r.severity(n)
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if k.Value == "severity" {
				if err := r.severity(v); err != nil {
					return err
				}
				continue
			}
			var val int
			if err := v.Decode(&val); err != nil {
				return fmt.Errorf("line %d: option %q must be a whole number", v.Line, k.Value)
			}
			if r.Options == nil {
				r.Options = map[string]int{}
			}
			r.Options[k.Value] = val
		}
		return nil
	}
	return fmt.Errorf("line %d: configure a rule with off, a severity, or a mapping", n.Line)
}

// severity reads the node's text rather than decoding it: YAML 1.1 readers
// take a bare `off` as false, and this should mean the word.
func (r *RuleConfig) severity(n *yaml.Node) error {
	if n.Value == "off" {
		r.Off = true
		return nil
	}
	if s := engine.ParseSeverity(n.Value); s != 0 {
		r.Severity = s
		return nil
	}
	return fmt.Errorf("line %d: severity must be off, error, warning or info, not %q", n.Line, n.Value)
}

// Ignore accepts a rule's findings in some operations, some files, or both.
// The reason is required: an ignore nobody can explain is one nobody can
// safely remove.
type Ignore struct {
	Rule string `yaml:"rule"`
	// Operations are patterns over labels such as "GET /v1/stats/summary".
	// `*` matches within one path segment, so "GET /v1/stats/*" matches
	// "GET /v1/stats/summary" but not "GET /v1/stats/a/b", and
	// "* /admin/*" matches every method.
	Operations []string `yaml:"operations"`
	// Files are patterns over the file a finding is in, written relative to
	// the config file's directory: "schemas/legacy.yaml", "schemas/**".
	// `**` matches any number of directories. URLs are matched as written.
	// The root document is a file too, so this also reaches findings that
	// belong to no operation.
	Files   []string `yaml:"files"`
	Reason  string   `yaml:"reason"`
	Line    int      `yaml:"-"`
	matched int
}

// matches applies an ignore to a finding. Operations and files each narrow
// it, so both given means both must match.
func (ig *Ignore) matches(op, file string) bool {
	if len(ig.Operations) > 0 && !slices.ContainsFunc(ig.Operations, func(p string) bool {
		ok, _ := path.Match(p, op)
		return ok
	}) {
		return false
	}
	if len(ig.Files) > 0 && !slices.ContainsFunc(ig.Files, func(p string) bool { return matchPath(p, file) }) {
		return false
	}
	return true
}

// matchPath matches a slash-separated name against a pattern in which `*`
// and friends work within one segment, as in path.Match, and a `**` segment
// matches any number of segments, including none.
func matchPath(pattern, name string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func matchSegments(p, n []string) bool {
	for len(p) > 0 {
		if p[0] == "**" {
			for i := 0; i <= len(n); i++ {
				if matchSegments(p[1:], n[i:]) {
					return true
				}
			}
			return false
		}
		if len(n) == 0 {
			return false
		}
		if ok, _ := path.Match(p[0], n[0]); !ok {
			return false
		}
		p, n = p[1:], n[1:]
	}
	return len(n) == 0
}

// UnmarshalYAML records the line, so problems can point at the entry.
func (ig *Ignore) UnmarshalYAML(n *yaml.Node) error {
	type plain Ignore
	var p plain
	if err := n.Decode(&p); err != nil {
		return err
	}
	*ig = Ignore(p)
	ig.Line = n.Line
	return nil
}

// Find looks for a config file in dir and then in each parent, stopping
// after the first directory that holds a .git: a config above the
// repository belongs to something else. It returns "" when there is none.
func Find(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		for _, name := range Names {
			p := filepath.Join(dir, name)
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				return p, nil
			}
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return "", nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}

// RepoRoot returns the nearest directory at or above dir that holds a .git,
// or "" when there is none.
func RepoRoot(dir string) string {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// Load reads and checks a config file. Unknown keys are errors: a misspelt
// `fail-undr` that silently does nothing is a gate someone believes is on.
func Load(p string) (*Config, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	c := &Config{}
	if err := dec.Decode(c); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	c.Path = p
	if err := c.check(); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	return c, nil
}

func (c *Config) check() error {
	if c.FailOn != "" && c.FailOn != "none" && engine.ParseSeverity(c.FailOn) == 0 {
		return fmt.Errorf("fail-on must be error, warning, info or none, not %q", c.FailOn)
	}
	if c.FailUnder != nil && (*c.FailUnder < 0 || *c.FailUnder > 100) {
		return fmt.Errorf("fail-under must be between 0 and 100, not %v", *c.FailUnder)
	}
	for _, ig := range c.Ignore {
		switch {
		case ig.Rule == "":
			return fmt.Errorf("line %d: an ignore needs a rule", ig.Line)
		case len(ig.Operations) == 0 && len(ig.Files) == 0:
			return fmt.Errorf("line %d: an ignore needs operations or files; to ignore a rule everywhere, set it to off under rules", ig.Line)
		case strings.TrimSpace(ig.Reason) == "":
			return fmt.Errorf("line %d: an ignore needs a reason", ig.Line)
		}
		for _, pat := range ig.Operations {
			if _, err := path.Match(pat, ""); err != nil {
				return fmt.Errorf("line %d: bad operation pattern %q: %w", ig.Line, pat, err)
			}
		}
		for _, pat := range ig.Files {
			for _, seg := range strings.Split(pat, "/") {
				if _, err := path.Match(seg, ""); err != nil {
					return fmt.Errorf("line %d: bad file pattern %q: %w", ig.Line, pat, err)
				}
			}
		}
	}
	return nil
}

// Apply returns the ruleset as this config adjusts it: rules set to off
// removed, severities and options overridden. The rules passed in are not
// modified. Every rule id and option named must exist, so a typo is an
// error rather than a setting that silently does nothing.
func (c *Config) Apply(rules []*engine.Rule) ([]*engine.Rule, error) {
	byID := map[string]*engine.Rule{}
	for _, r := range rules {
		byID[r.ID] = r
	}
	for id, rc := range c.Rules {
		r, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("%s:%d: unknown rule %q", c.Path, rc.Line, id)
		}
		for name := range rc.Options {
			if _, ok := r.Options[name]; !ok {
				return nil, fmt.Errorf("%s:%d: rule %s has no option %q%s", c.Path, rc.Line, id, name, optionList(r))
			}
		}
	}
	for _, ig := range c.Ignore {
		if _, ok := byID[ig.Rule]; !ok {
			return nil, fmt.Errorf("%s:%d: ignore names unknown rule %q", c.Path, ig.Line, ig.Rule)
		}
	}
	var out []*engine.Rule
	for _, r := range rules {
		rc, ok := c.Rules[r.ID]
		if ok && rc.Off {
			continue
		}
		r = r.Clone()
		if ok {
			if rc.Severity != 0 {
				r.Severity = rc.Severity
			}
			for k, v := range rc.Options {
				r.Options[k] = v
			}
		}
		out = append(out, r)
	}
	return out, nil
}

func optionList(r *engine.Rule) string {
	if len(r.Options) == 0 {
		return "; it has none"
	}
	var names []string
	for k := range r.Options {
		names = append(names, k)
	}
	slices.Sort(names)
	return "; it has " + strings.Join(names, ", ")
}

// Ignores returns the function engine.Options.Ignore wants. root names the
// root document as it was loaded, for findings that are in it. It counts
// matches, for Unused.
func (c *Config) Ignores(root string) func(engine.Finding) bool {
	return func(f engine.Finding) bool {
		file := c.relative(cmp.Or(f.File, root))
		for i := range c.Ignore {
			ig := &c.Ignore[i]
			if ig.Rule == f.Rule && ig.matches(f.Operation, file) {
				ig.matched++
				return true
			}
		}
		return false
	}
}

// relative expresses a finding's file the way ignore patterns are written:
// relative to the config file's directory, with forward slashes. That makes
// a pattern mean the same file whichever directory legible is run from.
// URLs are left as they are.
func (c *Config) relative(name string) string {
	if strings.HasPrefix(name, "http://") || strings.HasPrefix(name, "https://") || c.Path == "" {
		return filepath.ToSlash(name)
	}
	abs, err := filepath.Abs(name)
	if err != nil {
		return filepath.ToSlash(name)
	}
	base, err := filepath.Abs(filepath.Dir(c.Path))
	if err != nil {
		return filepath.ToSlash(abs)
	}
	rel, err := filepath.Rel(base, abs)
	if err != nil {
		return filepath.ToSlash(abs)
	}
	return filepath.ToSlash(rel)
}

// Unused returns the ignores that matched nothing in the last run, among
// those whose rule ran. A stale ignore is worth deleting: it will silently
// swallow the finding if the problem ever comes back.
func (c *Config) Unused(ran func(rule string) bool) []Ignore {
	var out []Ignore
	for _, ig := range c.Ignore {
		if ig.matched == 0 && ran(ig.Rule) {
			out = append(out, ig)
		}
	}
	return out
}
