package rules

import (
	"github.com/JustSteveKing/legible/engine"
	"github.com/JustSteveKing/legible/spec"
	"go.yaml.in/yaml/v3"
)

// examples reports whether a media type object, or the schema it carries,
// provides an example of the whole payload.
//
// An example can sit at any level. One on the media type or the root schema
// covers everything; one on a nested object covers that object; and a schema
// whose every leaf field carries its own example counts as well, because
// documentation tools and tool generators assemble those into a whole. That
// last form is common in generated specs, which put examples on the fields
// they describe rather than hand-writing whole payloads.
//
// Whether a schema is fully exampled is memoised per resolved node, so a
// component shared by every response is judged once. Walking it afresh for
// each response took 3.5 seconds on Stripe's spec.
type examples struct {
	doc  *spec.Document
	memo map[*yaml.Node]bool
	busy map[*yaml.Node]bool
}

func newExamples(doc *spec.Document) *examples {
	return &examples{doc: doc, memo: map[*yaml.Node]bool{}, busy: map[*yaml.Node]bool{}}
}

func (e *examples) has(media *yaml.Node) bool {
	if media == nil {
		return false
	}
	if exampled(media) {
		return true
	}
	schema := spec.Get(media, "schema")
	return schema != nil && e.complete(schema)
}

// complete reports whether a schema is shown in full by examples: one of its
// own, or examples covering every child. A schema with no children and no
// example is not.
func (e *examples) complete(site *yaml.Node) bool {
	if exampled(site) {
		return true
	}
	n, _ := e.doc.Resolve(site)
	if n == nil {
		return false
	}
	if exampled(n) || spec.Ref(n) != "" {
		// A part that did not resolve cannot be judged, and is not this
		// rule's to report; it is not held against the rest.
		return true
	}
	if v, ok := e.memo[n]; ok {
		return v
	}
	if e.busy[n] {
		return true // a cycle is judged where it starts, not where it closes
	}
	e.busy[n] = true
	children, all := 0, true
	visit := func(child *yaml.Node) {
		children++
		all = e.complete(child) && all
	}
	spec.Pairs(spec.Get(n, "properties"), func(_ string, _, v *yaml.Node) { visit(v) })
	for _, kw := range []string{"items", "additionalProperties"} {
		if c := spec.Get(n, kw); c != nil && c.Kind == yaml.MappingNode {
			visit(c)
		}
	}
	for _, kw := range []string{"allOf", "oneOf", "anyOf", "prefixItems"} {
		for _, c := range spec.Items(spec.Get(n, kw)) {
			visit(c)
		}
	}
	delete(e.busy, n)
	e.memo[n] = children > 0 && all
	return e.memo[n]
}

// unresolvedSchema reports whether a media type's schema is a $ref that did
// not resolve. Such a body is unresolved-ref's to report, or was skipped, and
// whether it has an example cannot be known.
func (e *examples) unresolvedSchema(media *yaml.Node) bool {
	s := spec.Get(media, "schema")
	if s == nil || exampled(media) {
		return false
	}
	n, _ := e.doc.Resolve(s)
	return spec.Ref(n) != ""
}

func exampled(n *yaml.Node) bool {
	return spec.Get(n, "example") != nil || spec.Get(n, "examples") != nil
}

func requestExample(ctx *engine.Context) {
	ex := newExamples(ctx.Doc)
	for _, op := range ctx.Doc.Operations() {
		body, _ := ctx.Doc.Resolve(spec.Get(op.Node, "requestBody"))
		if body == nil {
			continue
		}
		ptr := op.Pointer + "/requestBody/content"
		media, mediaPtr := spec.JSONMedia(spec.Get(body, "content"), ptr)
		if media == nil || ex.unresolvedSchema(media) {
			continue // request-body-schema or unresolved-ref reports these
		}
		ctx.Check(ex.has(media), op.Label(), media, mediaPtr,
			"request body has no example: an example is the fastest way for a model to see what a valid call looks like")
	}
}

func responseExample(ctx *engine.Context) {
	ex := newExamples(ctx.Doc)
	for _, op := range ctx.Doc.Operations() {
		spec.Pairs(spec.Get(op.Node, "responses"), func(code string, _, raw *yaml.Node) {
			if !isSuccess(code) {
				return
			}
			resp, _ := ctx.Doc.Resolve(raw)
			ptr := spec.Join(op.Pointer+"/responses", code) + "/content"
			media, mediaPtr := spec.JSONMedia(spec.Get(resp, "content"), ptr)
			if media == nil || ex.unresolvedSchema(media) {
				return // 204 and friends have nothing to exemplify
			}
			ctx.Check(ex.has(media), op.Label(), media, mediaPtr,
				code+" response has no example: an agent planning its next call cannot see what it will get back")
		})
	}
}
