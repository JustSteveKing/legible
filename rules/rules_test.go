package rules

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/JustSteveKing/legible/engine"
	"github.com/JustSteveKing/legible/spec"
)

// The guide is embedded from docs/, so a rule without a page would make
// `legible explain` print nothing, and a page for a deleted rule would be
// documentation for something that no longer exists. The heading and the
// category line are checked too, because a severity changed in code and not
// in the guide is a guide that lies.
func TestEveryRuleHasAGuide(t *testing.T) {
	ids := map[string]bool{}
	for _, r := range All() {
		if ids[r.ID] {
			t.Errorf("rule id %q is used twice", r.ID)
		}
		ids[r.ID] = true
		g := Guide(r.ID)
		if g == "" {
			t.Errorf("rule %q has no docs/%s.md", r.ID, r.ID)
			continue
		}
		if !strings.HasPrefix(g, "# "+r.ID+"\n") {
			t.Errorf("docs/%s.md does not start with its rule id as the heading", r.ID)
		}
		if want := "**" + r.Category + "** · " + r.Severity.String(); !strings.Contains(g, want) {
			t.Errorf("docs/%s.md does not state %q", r.ID, want)
		}
		// An option nobody can find is an option nobody sets. Each must be
		// documented with its default, as it would be written in config.
		for name, def := range r.Options {
			if want := fmt.Sprintf("`%s: %d`", name, def); !strings.Contains(g, want) {
				t.Errorf("docs/%s.md does not document option %s with its default, as %s", r.ID, name, want)
			}
		}
	}
	entries, err := docs.ReadDir("docs")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if id := strings.TrimSuffix(e.Name(), ".md"); id != "index" && !ids[id] {
			t.Errorf("docs/%s documents a rule that does not exist", e.Name())
		}
	}
	if Guide("index") == "" {
		t.Error("docs/index.md, the overview `legible explain` prints, is missing")
	}
}

func load(t *testing.T, path string) *spec.Document {
	t.Helper()
	doc, err := spec.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestGoodSpecHasNoFindings(t *testing.T) {
	rep := engine.Run(load(t, "testdata/good.yaml"), All())
	for _, f := range rep.Findings() {
		t.Errorf("%s at line %d: %s", f.Rule, f.Line, f.Message)
	}
	if rep.Score != 100 {
		t.Errorf("score = %.1f, want 100", rep.Score)
	}
}

// findings returns "rule operation" for every finding, for asserting on real
// specs without depending on message wording.
func findings(rep *engine.Report) []string {
	var out []string
	for _, f := range rep.Findings() {
		out = append(out, f.Rule+" "+f.Operation)
	}
	return out
}

// The real specs pin findings that were checked by hand against the
// document, so a rule change that loses one is caught.
func TestPetstore(t *testing.T) {
	got := findings(engine.Run(load(t, "../testdata/petstore.json"), All()))
	for _, want := range []string{
		// username is both the path parameter and a User property.
		"argument-collision PUT /user/{username}",
		// "This can only be done by the logged in user." on all three.
		"duplicate-description POST /user",
		"duplicate-description PUT /user/{username}",
		"duplicate-description DELETE /user/{username}",
		"description-restates-name DELETE /pet/{petId}",
		// The store and user operations declare no security at all.
		"security-declared POST /store/order",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("missing finding %q", want)
		}
	}
	if slices.Contains(got, "security-declared GET /pet/{petId}") {
		t.Error("GET /pet/{petId} declares api_key and petstore_auth, but was reported as having no security")
	}
}

// pricepaid is a well-kept spec, generated from its routes. It should have
// no errors, and its wide search endpoints should trip parameter-count, which
// is the one warning its own maintainer has decided to accept.
func TestPricepaid(t *testing.T) {
	rep := engine.Run(load(t, "../testdata/pricepaid.json"), All())
	for _, f := range rep.Findings() {
		if f.Severity == engine.Error {
			t.Errorf("unexpected error: %s %s: %s", f.Rule, f.Operation, f.Message)
		}
	}
	if !slices.Contains(findings(rep), "parameter-count GET /v1/transactions") {
		t.Error("GET /v1/transactions takes 29 parameters but parameter-count did not report it")
	}
}

// Two documented top-level properties and one undocumented one level down:
// 1 − 0.5/2.5 = 0.8. Counted equally it would be 2/3.
func TestNestedPropertiesWeighLess(t *testing.T) {
	doc, err := spec.Parse("case.yaml", []byte(header+`
paths:
  /a:
    post:
      operationId: a
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                a: {type: string, description: Top.}
                b:
                  type: object
                  description: Also top.
                  properties:
                    c: {type: string}
`))
	if err != nil {
		t.Fatal(err)
	}
	res := engine.Run(doc, []*engine.Rule{Find("property-description")}).Rules[0]
	if res.Checked != 3 || res.Failed != 1 || res.Score() < 0.799 || res.Score() > 0.801 {
		t.Errorf("checked %d failed %d score %.3f; want 3, 1 and 0.8", res.Checked, res.Failed, res.Score())
	}
	for depth, want := range map[int]float64{0: 1, 1: 1, 2: 0.5, 3: 0.25} {
		if got := nestedWeight(depth); got != want {
			t.Errorf("nestedWeight(%d) = %v, want %v", depth, got, want)
		}
	}
}

func TestWords(t *testing.T) {
	for in, want := range map[string]string{
		"getPetById":         "get pet by id",
		"get-pet-by-id":      "get pet by id",
		"HTTPServerError":    "http server error",
		"page_size":          "page size",
		"v1Items":            "v1 items",
		"Returns a pet.":     "returns a pet",
		"/pets/{petId}/tags": "pets pet id tags",
	} {
		if got := strings.Join(words(in), " "); got != want {
			t.Errorf("words(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSentences(t *testing.T) {
	for in, want := range map[string]int{
		"":                              0,
		"Lists pets":                    1,
		"Lists pets.":                   1,
		"Lists pets. Use it to browse.": 2,
		"Costs 2.50 each. Really?":      2,
		"Lists pets. Newest first":      2,
	} {
		if got := sentences(in); got != want {
			t.Errorf("sentences(%q) = %d, want %d", in, got, want)
		}
	}
}
