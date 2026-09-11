package rules

import (
	"os"
	"testing"
	"time"

	"github.com/JustSteveKing/legible/engine"
	"github.com/JustSteveKing/legible/spec"
)

// TestLargeSpec runs every rule over a large real spec and fails any rule
// that takes longer than its budget. The spec is too big to commit, so the
// test runs only when LEGIBLE_LARGE_SPEC names one, e.g. Stripe's:
//
//	curl -sLo /tmp/stripe.json https://raw.githubusercontent.com/stripe/openapi/master/openapi/spec3.json
//	LEGIBLE_LARGE_SPEC=/tmp/stripe.json go test ./rules/ -run TestLargeSpec -v
//
// Each rule gets its own deadline, so one that goes exponential is named
// rather than hanging the whole run.
func TestLargeSpec(t *testing.T) {
	path := os.Getenv("LEGIBLE_LARGE_SPEC")
	if path == "" {
		t.Skip("set LEGIBLE_LARGE_SPEC to a large OpenAPI document to run")
	}
	start := time.Now()
	doc, err := spec.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("load: %v, %d operations", time.Since(start).Round(time.Millisecond), len(doc.Operations()))

	const budget = 20 * time.Second
	for _, r := range All() {
		done := make(chan engine.RuleResult, 1)
		start := time.Now()
		go func() { done <- engine.Run(doc, []*engine.Rule{r}).Rules[0] }()
		select {
		case res := <-done:
			t.Logf("%-28s %8v  checked %6d  failed %6d", r.ID, time.Since(start).Round(time.Millisecond), res.Checked, res.Failed)
		case <-time.After(budget):
			t.Errorf("%-28s did not finish within %v", r.ID, budget)
		}
	}
}
