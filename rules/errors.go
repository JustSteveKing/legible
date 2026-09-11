package rules

import (
	"fmt"
	"slices"
	"strings"

	"github.com/JustSteveKing/legible/engine"
	"github.com/JustSteveKing/legible/spec"
	"go.yaml.in/yaml/v3"
)

func errorResponses(ctx *engine.Context) {
	for _, op := range ctx.Doc.Operations() {
		found := false
		spec.Pairs(spec.Get(op.Node, "responses"), func(code string, _, _ *yaml.Node) {
			found = found || isError(code)
		})
		ctx.Check(found, op.Label(), op.Node, op.Pointer+"/responses",
			"no 4xx, 5xx or default response: an agent that gets an error has nothing to tell it what went wrong or whether to retry")
	}
}

func errorSchema(ctx *engine.Context) {
	seen := map[*yaml.Node]bool{} // a shared components/responses entry is reported once
	for _, op := range ctx.Doc.Operations() {
		jsonResponses(ctx.Doc, op, func(code string, resp, schema *yaml.Node, ptr, _ string) {
			if !isError(code) || seen[resp] {
				return
			}
			seen[resp] = true
			ctx.Check(schema != nil, op.Label(), resp, ptr,
				fmt.Sprintf("%s response has no body schema: an agent sees the status code and nothing it can act on", code))
		})
	}
}

// shapeOf summarises an error schema by its top-level property names, which
// is what an agent reads to find the message. Two schemas with different
// names but the same fields are the same shape to a reader.
func shapeOf(doc *spec.Document, schema *yaml.Node) string {
	var keys []string
	doc.Walk(schema, "", func(s spec.SchemaNode) bool {
		if s.Depth == 1 {
			keys = append(keys, s.Property)
		}
		return s.Depth == 0
	})
	if len(keys) == 0 {
		return ""
	}
	slices.Sort(keys)
	return strings.Join(slices.Compact(keys), ",")
}

func errorShape(ctx *engine.Context) {
	type use struct {
		shape, op, ptr string
		node           *yaml.Node
	}
	var uses []use
	counts := map[string]int{}
	seen := map[*yaml.Node]bool{}
	for _, op := range ctx.Doc.Operations() {
		jsonResponses(ctx.Doc, op, func(code string, resp, schema *yaml.Node, _, schemaPtr string) {
			if !isError(code) || schema == nil || seen[resp] {
				return
			}
			seen[resp] = true
			shape := shapeOf(ctx.Doc, schema)
			if shape == "" {
				return
			}
			uses = append(uses, use{shape, op.Label(), schemaPtr, schema})
			counts[shape]++
		})
	}
	if len(counts) == 0 {
		return
	}
	dominant := ""
	for _, u := range uses { // first-seen wins ties, so output is stable
		if counts[u.shape] > counts[dominant] {
			dominant = u.shape
		}
	}
	for _, u := range uses {
		ctx.Check(u.shape == dominant, u.op, u.node, u.ptr,
			fmt.Sprintf("error body has fields {%s} where %d other error responses use {%s}", u.shape, counts[dominant], dominant))
	}
}
