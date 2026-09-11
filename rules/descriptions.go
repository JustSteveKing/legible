package rules

import (
	"fmt"
	"strings"

	"github.com/JustSteveKing/legible/engine"
	"github.com/JustSteveKing/legible/spec"
	"go.yaml.in/yaml/v3"
)

// minSentences is the shortest description that passes operation-description.
// Anthropic's tool-definition guidance asks for "at least 3–4 sentences for
// each tool description"; two is the floor below which there is no room to say
// both what an operation does and when to choose it over its neighbours.
const minSentences = 2

func operationDescription(ctx *engine.Context) {
	for _, op := range ctx.Doc.Operations() {
		text, node := describe(op)
		switch {
		case text == "":
			ctx.Fail(op.Label(), op.Node, op.Pointer, "no description or summary: an agent choosing between tools has only the name to go on")
		case spec.Str(op.Node, "description") == "":
			ctx.Fail(op.Label(), node, op.Pointer+"/summary", "summary only: a summary names the operation, a description is what tells an agent when to use it")
		default:
			n := sentences(text)
			ctx.Check(n >= ctx.Option("min-sentences"), op.Label(), node, op.Pointer+"/description",
				fmt.Sprintf("description is %d sentence%s: say what it does, when to use it, and what it returns", n, plural(n)))
		}
	}
}

// filler is vocabulary that carries no information about an operation beyond
// what its name and method already say.
var filler = set("a", "an", "the", "of", "by", "for", "to", "from", "with", "and", "or", "in", "on", "at",
	"this", "that", "it", "is", "are", "be", "will", "given", "specified", "provided", "single", "one",
	"get", "gets", "getting", "fetch", "fetches", "retrieve", "retrieves", "return", "returns", "read", "reads",
	"list", "lists", "all", "find", "finds", "create", "creates", "add", "adds", "new", "update", "updates",
	"modify", "modifies", "edit", "delete", "deletes", "remove", "removes", "set", "sets", "put", "post", "patch",
	"data", "info", "information", "details", "record", "records", "object", "objects", "item", "items",
	"endpoint", "api", "operation", "method", "request", "resource", "resources", "id", "ids")

// restatementMaxWords bounds description-restates-name. Past this length a
// description is saying something even if it reuses the name's words.
const restatementMaxWords = 12

func descriptionRestatesName(ctx *engine.Context) {
	for _, op := range ctx.Doc.Operations() {
		text, node := describe(op)
		if text == "" {
			continue // operation-description already reports it
		}
		name := set(words(op.ID)...)
		for _, seg := range strings.Split(op.Path, "/") {
			for _, w := range words(seg) {
				name[w] = true
				name[strings.TrimSuffix(w, "s")] = true
			}
		}
		ws := words(text)
		novel := 0
		for _, w := range ws {
			if !filler[w] && !name[w] && !name[strings.TrimSuffix(w, "s")] {
				novel++
			}
		}
		ptr := op.Pointer + "/description"
		if spec.Str(op.Node, "description") == "" {
			ptr = op.Pointer + "/summary"
		}
		ctx.Check(len(ws) > restatementMaxWords || novel > 0, op.Label(), node, ptr,
			fmt.Sprintf("%q only restates the operation's name and path", strings.TrimSpace(text)))
	}
}

func parameterDescription(ctx *engine.Context) {
	seen := map[string]bool{} // a path-level parameter is shared by every operation under it
	for _, op := range ctx.Doc.Operations() {
		for _, p := range ctx.Doc.Parameters(op) {
			if seen[p.Pointer] {
				continue
			}
			seen[p.Pointer] = true
			ctx.Check(paramDescription(p) != "", op.Label(), p.Node, p.Pointer,
				fmt.Sprintf("%s parameter %q has no description", p.In, p.Name))
		}
	}
}

func propertyDescription(ctx *engine.Context) {
	seen := map[*yaml.Node]bool{} // shared components are reported once
	for _, op := range ctx.Doc.Operations() {
		schema, ptr, _ := ctx.Doc.RequestSchema(op)
		ctx.Doc.Walk(schema, ptr, func(s spec.SchemaNode) bool {
			if s.Property == "" || seen[s.Site] {
				return !seen[s.Site]
			}
			seen[s.Site] = true
			if spec.Ref(s.Node) != "" && strings.TrimSpace(spec.Str(s.Site, "description")) == "" {
				return false // its description may be in the file that did not resolve
			}
			ctx.CheckWeighted(strings.TrimSpace(spec.Str(s.Node, "description")) != "" || strings.TrimSpace(spec.Str(s.Site, "description")) != "",
				nestedWeight(s.Depth), op.Label(), s.Site, s.Pointer, fmt.Sprintf("request property %q has no description", s.Property))
			return true
		})
	}
}

// nestedWeight is how much a request property counts towards a rule's score,
// by how deep it sits: 1 at the top level, halving with each level below. A
// model fills a tool's top-level arguments on every call, and meets a field
// three objects down far less often. Counted equally, the 10,445
// undocumented nested form fields in Stripe's spec drowned out everything
// else property-description found. Arrays count as a level: tags[].name sits
// two below tags.
func nestedWeight(depth int) float64 {
	w := 1.0
	for i := 1; i < depth; i++ {
		w /= 2
	}
	return w
}

func duplicateDescription(ctx *engine.Context) {
	byText := map[string][]spec.Operation{}
	var order []string
	for _, op := range ctx.Doc.Operations() {
		text, _ := describe(op)
		key := strings.Join(words(text), " ")
		if key == "" {
			continue
		}
		if _, ok := byText[key]; !ok {
			order = append(order, key)
		}
		byText[key] = append(byText[key], op)
	}
	for _, key := range order {
		ops := byText[key]
		if len(ops) == 1 {
			ctx.Pass()
			continue
		}
		var labels []string
		for _, op := range ops {
			labels = append(labels, op.Label())
		}
		for _, op := range ops {
			_, node := describe(op)
			ctx.Fail(op.Label(), node, op.Pointer, fmt.Sprintf("described identically to %d other operation%s (%s): an agent cannot tell them apart",
				len(ops)-1, plural(len(ops)-1), strings.Join(labels, ", ")))
		}
	}
}

func set(ws ...string) map[string]bool {
	m := make(map[string]bool, len(ws))
	for _, w := range ws {
		m[w] = true
	}
	return m
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
