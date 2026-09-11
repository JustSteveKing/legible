package report

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/JustSteveKing/legible/engine"
)

// guideURL is where each rule's guide page is published, for SARIF viewers
// that link a finding to its explanation.
const guideURL = "https://github.com/JustSteveKing/legible/blob/main/rules/docs/"

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool        sarifTool         `json:"tool"`
	Invocations []sarifInvocation `json:"invocations"`
	Results     []sarifResult     `json:"results"`
}

// sarifInvocation carries what legible could not check. A skipped file is
// not a result, since nothing is wrong with the spec, but a reader of the
// results needs to know the coverage was partial.
type sarifInvocation struct {
	ExecutionSuccessful        bool                `json:"executionSuccessful"`
	ToolExecutionNotifications []sarifNotification `json:"toolExecutionNotifications,omitempty"`
}

type sarifNotification struct {
	Level   string    `json:"level"`
	Message sarifText `json:"message"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version,omitempty"`
	InformationURI string      `json:"informationUri"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID                   string       `json:"id"`
	Name                 string       `json:"name"`
	ShortDescription     sarifText    `json:"shortDescription"`
	HelpURI              string       `json:"helpUri"`
	DefaultConfiguration sarifConfig  `json:"defaultConfiguration"`
	Properties           sarifRuleTag `json:"properties"`
}

type sarifRuleTag struct {
	Tags []string `json:"tags"`
}

type sarifConfig struct {
	Level string `json:"level"`
}

type sarifText struct {
	Text string `json:"text"`
}

type sarifResult struct {
	RuleID       string             `json:"ruleId"`
	RuleIndex    int                `json:"ruleIndex"`
	Level        string             `json:"level"`
	Message      sarifText          `json:"message"`
	Locations    []sarifLocation    `json:"locations"`
	Suppressions []sarifSuppression `json:"suppressions,omitempty"`
}

// sarifSuppression marks a finding accepted in .legible.yaml. It is still
// reported, so code scanning shows it as dismissed rather than as never
// having existed.
type sarifSuppression struct {
	Kind string `json:"kind"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysical `json:"physicalLocation"`
}

type sarifPhysical struct {
	ArtifactLocation sarifArtifact `json:"artifactLocation"`
	Region           sarifRegion   `json:"region"`
}

type sarifArtifact struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn,omitempty"`
}

func level(s engine.Severity) string {
	switch s {
	case engine.Error:
		return "error"
	case engine.Warning:
		return "warning"
	}
	return "note"
}

// SARIF writes SARIF 2.1.0, which GitHub code scanning and most CI systems
// can annotate a pull request from.
func SARIF(w io.Writer, rep *engine.Report, opts Options) error {
	driver := sarifDriver{Name: "legible", Version: opts.Version, InformationURI: "https://github.com/JustSteveKing/legible", Rules: []sarifRule{}}
	index := map[string]int{}
	for i, r := range rep.Rules {
		index[r.Rule.ID] = i
		driver.Rules = append(driver.Rules, sarifRule{
			ID:                   r.Rule.ID,
			Name:                 r.Rule.Title,
			ShortDescription:     sarifText{r.Rule.Title},
			HelpURI:              guideURL + r.Rule.ID + ".md",
			DefaultConfiguration: sarifConfig{level(r.Rule.Severity)},
			Properties:           sarifRuleTag{Tags: []string{"agent-readiness", r.Rule.Category}},
		})
	}
	results := []sarifResult{}
	add := func(f engine.Finding, suppressed bool) {
		msg := f.Message
		if f.Operation != "" {
			msg = f.Operation + ": " + msg
		}
		line := f.Line
		if line < 1 {
			line = 1 // SARIF regions are 1-based and required to be positive
		}
		uri := rep.Document
		if f.File != "" {
			uri = f.File
		}
		r := sarifResult{
			RuleID:    f.Rule,
			RuleIndex: index[f.Rule],
			Level:     level(f.Severity),
			Message:   sarifText{msg},
			Locations: []sarifLocation{{sarifPhysical{sarifArtifact{uri}, sarifRegion{line, f.Column}}}},
		}
		if suppressed {
			r.Suppressions = []sarifSuppression{{Kind: "external"}}
		}
		results = append(results, r)
	}
	for _, f := range rep.Findings() {
		add(f, false)
	}
	for _, f := range rep.Suppressed() {
		add(f, true)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	inv := sarifInvocation{ExecutionSuccessful: true}
	for _, s := range rep.Skipped {
		inv.ToolExecutionNotifications = append(inv.ToolExecutionNotifications, sarifNotification{
			Level:   "warning",
			Message: sarifText{fmt.Sprintf("Not checked: %s could not be fetched (%s), so the schemas behind it were not checked.", s.URL, s.Reason)},
		})
	}
	for _, u := range rep.Stale {
		inv.ToolExecutionNotifications = append(inv.ToolExecutionNotifications, sarifNotification{
			Level:   "note",
			Message: sarifText{fmt.Sprintf("%s could not be fetched this run; the copy cached by an earlier run was checked.", u)},
		})
	}
	return enc.Encode(sarifLog{
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Version: "2.1.0",
		Runs:    []sarifRun{{Tool: sarifTool{driver}, Invocations: []sarifInvocation{inv}, Results: results}},
	})
}
