// Package rules is legible's agent-readiness ruleset.
//
// Every rule has a guide page in docs/, embedded into the binary so that
// `legible explain <rule>` works offline and the guide cannot drift from the
// rules it describes. TestEveryRuleHasAGuide holds that in place.
package rules

import (
	"embed"
	"html"
	"regexp"
	"strings"
	"unicode"

	"github.com/JustSteveKing/legible/engine"
	"github.com/JustSteveKing/legible/spec"
	"go.yaml.in/yaml/v3"
)

// Categories, in the order reports show them.
const (
	Descriptions = "descriptions"
	Naming       = "naming"
	Errors       = "errors"
	Examples     = "examples"
	ToolSchema   = "tool-schema"
	Safety       = "safety"
)

//go:embed docs/*.md
var docs embed.FS

// All returns the ruleset in report order. It returns fresh values each
// call, so callers may adjust severities without affecting anyone else.
func All() []*engine.Rule {
	return []*engine.Rule{
		// descriptions
		{ID: "operation-description", Title: "Operations have a description", Category: Descriptions, Severity: engine.Warning, Options: map[string]int{"min-sentences": minSentences}, Check: operationDescription},
		{ID: "description-restates-name", Title: "Descriptions say more than the name", Category: Descriptions, Severity: engine.Warning, Check: descriptionRestatesName},
		{ID: "parameter-description", Title: "Parameters have a description", Category: Descriptions, Severity: engine.Warning, Check: parameterDescription},
		{ID: "property-description", Title: "Request properties have a description", Category: Descriptions, Severity: engine.Info, Check: propertyDescription},
		{ID: "duplicate-description", Title: "Operations are distinguishable", Category: Descriptions, Severity: engine.Warning, Check: duplicateDescription},
		// naming
		{ID: "operation-id", Title: "Operations have a unique operationId", Category: Naming, Severity: engine.Error, Check: operationID},
		{ID: "tool-name", Title: "operationIds are valid tool names", Category: Naming, Severity: engine.Error, Check: toolName},
		{ID: "naming-style", Title: "Names use one casing style", Category: Naming, Severity: engine.Warning, Check: namingStyle},
		// errors
		{ID: "error-responses", Title: "Operations document their errors", Category: Errors, Severity: engine.Warning, Check: errorResponses},
		{ID: "error-schema", Title: "Error responses have a body schema", Category: Errors, Severity: engine.Warning, Check: errorSchema},
		{ID: "error-shape", Title: "Errors share one shape", Category: Errors, Severity: engine.Info, Check: errorShape},
		// examples
		{ID: "request-example", Title: "Request bodies have an example", Category: Examples, Severity: engine.Info, Check: requestExample},
		{ID: "response-example", Title: "Success responses have an example", Category: Examples, Severity: engine.Info, Check: responseExample},
		// tool-schema
		{ID: "unresolved-ref", Title: "Every $ref resolves", Category: ToolSchema, Severity: engine.Error, Check: unresolvedRef},
		{ID: "request-body-schema", Title: "Request bodies are typed", Category: ToolSchema, Severity: engine.Error, Check: requestBodySchema},
		{ID: "recursive-schema", Title: "Request schemas are not recursive", Category: ToolSchema, Severity: engine.Error, Check: recursiveSchema},
		{ID: "argument-collision", Title: "Arguments flatten without collisions", Category: ToolSchema, Severity: engine.Error, Check: argumentCollision},
		{ID: "schema-depth", Title: "Request schemas are shallow", Category: ToolSchema, Severity: engine.Warning, Options: map[string]int{"max": maxDepth}, Check: schemaDepth},
		{ID: "parameter-count", Title: "Operations take a manageable number of arguments", Category: ToolSchema, Severity: engine.Warning, Options: map[string]int{"max": maxArguments}, Check: parameterCount},
		{ID: "untyped-property", Title: "Request properties are typed", Category: ToolSchema, Severity: engine.Warning, Check: untypedProperty},
		{ID: "polymorphism-discriminator", Title: "Unions in requests have a discriminator", Category: ToolSchema, Severity: engine.Warning, Check: polymorphismDiscriminator},
		// safety
		{ID: "security-declared", Title: "Operations declare their auth", Category: Safety, Severity: engine.Warning, Check: securityDeclared},
	}
}

// Guide returns the guide page for a rule, or "" if there is none.
func Guide(id string) string {
	b, err := docs.ReadFile("docs/" + id + ".md")
	if err != nil {
		return ""
	}
	return string(b)
}

// Find returns the rule with the given id, or nil.
func Find(id string) *engine.Rule {
	for _, r := range All() {
		if r.ID == id {
			return r
		}
	}
	return nil
}

// describe returns the text an agent would read for an operation: the
// description when there is one, else the summary. Tool generators almost
// all fall back this way. The text is plain: see plain.
func describe(op spec.Operation) (text string, node *yaml.Node) {
	if d := spec.Get(op.Node, "description"); d != nil && plain(d.Value) != "" {
		return plain(d.Value), d
	}
	if s := spec.Get(op.Node, "summary"); s != nil && plain(s.Value) != "" {
		return plain(s.Value), s
	}
	return "", nil
}

// paramDescription returns a parameter's description, falling back to its
// schema's, since either reaches the agent.
func paramDescription(p spec.Param) string {
	if d := plain(spec.Str(p.Node, "description")); d != "" {
		return d
	}
	return plain(spec.Str(p.Schema, "description"))
}

var htmlTag = regexp.MustCompile(`<[^>]*>`)

// plain strips HTML tags and entities from a description. Some large specs,
// Stripe's among them, write descriptions as HTML. Left in, `<p>` makes "p" a
// word and hides the full stops the sentence counter looks for.
func plain(s string) string {
	s = htmlTag.ReplaceAllString(s, " ")
	return strings.TrimSpace(strings.Join(strings.Fields(html.UnescapeString(s)), " "))
}

// words splits text into lower-case words, breaking identifiers at case
// changes as well as punctuation: "getPetById" and "get-pet-by-id" both
// become [get pet by id].
func words(s string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	rs := []rune(s)
	for i, r := range rs {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if unicode.IsUpper(r) && len(cur) > 0 {
				prevLower := unicode.IsLower(rs[i-1]) || unicode.IsDigit(rs[i-1])
				nextLower := i+1 < len(rs) && unicode.IsLower(rs[i+1])
				if prevLower || (unicode.IsUpper(rs[i-1]) && nextLower) {
					flush()
				}
			}
			cur = append(cur, r)
		default:
			flush()
		}
	}
	flush()
	return out
}

// sentences counts sentences roughly: terminal punctuation followed by space
// or the end, with a trailing fragment counting as one.
func sentences(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	n := 0
	rs := []rune(s)
	for i, r := range rs {
		if (r == '.' || r == '!' || r == '?') && (i+1 == len(rs) || unicode.IsSpace(rs[i+1])) {
			n++
		}
	}
	if last := rs[len(rs)-1]; last != '.' && last != '!' && last != '?' {
		n++
	}
	return n
}

// jsonResponses calls fn for each response of op, resolved, with its status
// code and the schema of its JSON body (nil when it has none).
func jsonResponses(doc *spec.Document, op spec.Operation, fn func(code string, resp, schema *yaml.Node, ptr, schemaPtr string)) {
	spec.Pairs(spec.Get(op.Node, "responses"), func(code string, _, raw *yaml.Node) {
		resp, _ := doc.Resolve(raw)
		ptr := spec.Join(op.Pointer+"/responses", code)
		mt, mtPtr := spec.JSONMedia(spec.Get(resp, "content"), ptr+"/content")
		var schema *yaml.Node
		if mt != nil {
			schema = spec.Get(mt, "schema")
		}
		fn(code, resp, schema, ptr, mtPtr+"/schema")
	})
}

func isError(code string) bool {
	return code == "default" || strings.HasPrefix(code, "4") || strings.HasPrefix(code, "5")
}

func isSuccess(code string) bool { return strings.HasPrefix(code, "2") }
