package rules

import (
	"fmt"
	"strings"

	"github.com/JustSteveKing/legible/engine"
	"github.com/JustSteveKing/legible/spec"
	"go.yaml.in/yaml/v3"
)

func requestBodySchema(ctx *engine.Context) {
	for _, op := range ctx.Doc.Operations() {
		schema, ptr, hasBody := ctx.Doc.RequestSchema(op)
		if !hasBody {
			continue
		}
		if schema == nil {
			ctx.Fail(op.Label(), spec.Get(op.Node, "requestBody"), op.Pointer+"/requestBody",
				"request body has no schema: there is nothing to turn into tool arguments")
			continue
		}
		n, _ := ctx.Doc.Resolve(schema)
		if spec.Ref(n) != "" {
			continue // it did not resolve: unresolved-ref reports it, or it was skipped
		}
		free := spec.Get(n, "properties") == nil && !composed(n) && spec.Get(n, "items") == nil &&
			spec.Get(n, "enum") == nil && spec.Str(n, "additionalProperties") != "false" &&
			(len(spec.Types(n)) == 0 || spec.HasType(n, "object"))
		ctx.Check(!free, op.Label(), schema, ptr,
			"request body is a free-form object: a model has to guess every field name")
	}
}

func composed(n *yaml.Node) bool {
	return spec.Get(n, "allOf") != nil || spec.Get(n, "oneOf") != nil || spec.Get(n, "anyOf") != nil
}

func recursiveSchema(ctx *engine.Context) {
	for _, op := range ctx.Doc.Operations() {
		schema, ptr, _ := ctx.Doc.RequestSchema(op)
		if schema == nil {
			continue
		}
		loops := ctx.Doc.Walk(schema, ptr, func(spec.SchemaNode) bool { return true })
		if len(loops) == 0 {
			ctx.Pass()
			continue
		}
		l := loops[0]
		ctx.Fail(op.Label(), l.Site, l.Pointer, fmt.Sprintf(
			"request schema is recursive (%s): it cannot be inlined into a tool's input schema, and strict tool use rejects it",
			strings.Join(l.Refs, " → ")))
	}
}

// arguments returns the names a tool built from op would take: every
// parameter, plus the top-level properties of the request body, which is how
// most generators flatten an operation into one argument object.
func arguments(doc *spec.Document, op spec.Operation) (params []spec.Param, props []spec.SchemaNode) {
	params = doc.Parameters(op)
	schema, ptr, _ := doc.RequestSchema(op)
	doc.Walk(schema, ptr, func(s spec.SchemaNode) bool {
		if s.Depth == 1 {
			props = append(props, s)
		}
		return s.Depth == 0
	})
	return params, props
}

func argumentCollision(ctx *engine.Context) {
	for _, op := range ctx.Doc.Operations() {
		params, props := arguments(ctx.Doc, op)
		where := map[string]string{}
		var clash []string
		note := func(name, source string) {
			if prev, ok := where[name]; ok && prev != source {
				clash = append(clash, fmt.Sprintf("%q (%s and %s)", name, prev, source))
				return
			}
			where[name] = source
		}
		for _, p := range params {
			note(p.Name, p.In)
		}
		for _, p := range props {
			note(p.Property, "body")
		}
		ctx.Check(len(clash) == 0, op.Label(), op.Node, op.Pointer,
			"arguments collide when flattened into one tool input: "+strings.Join(clash, ", "))
	}
}

// maxDepth is OpenAI's nesting limit for strict schemas. Past it the schema
// is rejected outright; well before it, models start misplacing fields.
const maxDepth = 5

func schemaDepth(ctx *engine.Context) {
	m := &depths{doc: ctx.Doc, memo: map[*yaml.Node]depth{}, busy: map[*yaml.Node]bool{}}
	for _, op := range ctx.Doc.Operations() {
		schema, ptr, _ := ctx.Doc.RequestSchema(op)
		if schema == nil {
			continue
		}
		d, limit := m.of(schema), ctx.Option("max")
		ctx.Check(d.levels <= limit, op.Label(), schema, ptr,
			fmt.Sprintf("request schema nests %d levels deep (%s): the limit is %d",
				d.levels, strings.Join(d.path, "."), limit))
	}
}

// depths computes how far objects and arrays nest below a schema, and the
// route to the deepest point. It is memoised per resolved node, so a shared
// component costs one visit however many routes reach it; spec.Walk's
// first-route depth would under-report a component reached two ways.
type depths struct {
	doc  *spec.Document
	memo map[*yaml.Node]depth
	busy map[*yaml.Node]bool
}

type depth struct {
	levels int
	path   []string
}

func (m *depths) of(site *yaml.Node) depth {
	n, _ := m.doc.Resolve(site)
	if n == nil {
		return depth{}
	}
	if d, ok := m.memo[n]; ok {
		return d
	}
	if m.busy[n] {
		return depth{} // recursion; recursive-schema reports it
	}
	m.busy[n] = true
	best := depth{}
	consider := func(child *yaml.Node, step string, levels int) {
		d := m.of(child)
		if d.levels+levels <= best.levels {
			return
		}
		path := d.path
		if step != "" {
			path = append([]string{step}, d.path...)
		}
		best = depth{d.levels + levels, path}
	}
	spec.Pairs(spec.Get(n, "properties"), func(name string, _, v *yaml.Node) { consider(v, name, 1) })
	if items := spec.Get(n, "items"); items != nil && items.Kind == yaml.MappingNode {
		consider(items, "[]", 1)
	}
	if ap := spec.Get(n, "additionalProperties"); ap != nil && ap.Kind == yaml.MappingNode {
		consider(ap, "{}", 1)
	}
	for _, kw := range []string{"allOf", "oneOf", "anyOf", "prefixItems"} {
		for _, s := range spec.Items(spec.Get(n, kw)) {
			consider(s, "", 0)
		}
	}
	delete(m.busy, n)
	m.memo[n] = best
	return best
}

// maxArguments is where parameter-count starts to warn. There is no hard
// limit; this is roughly where models begin dropping optional arguments and
// confusing similar ones, and it is the same order as the threshold Redocly's
// score uses for its parameter hotspots.
const maxArguments = 15

func parameterCount(ctx *engine.Context) {
	for _, op := range ctx.Doc.Operations() {
		params, props := arguments(ctx.Doc, op)
		n := len(params) + len(props)
		ctx.Check(n <= ctx.Option("max"), op.Label(), op.Node, op.Pointer,
			fmt.Sprintf("takes %d arguments (%d parameters, %d body properties): consider splitting it, or grouping rarely used ones", n, len(params), len(props)))
	}
}

func untypedProperty(ctx *engine.Context) {
	seen := map[*yaml.Node]bool{}
	for _, op := range ctx.Doc.Operations() {
		schema, ptr, _ := ctx.Doc.RequestSchema(op)
		ctx.Doc.Walk(schema, ptr, func(s spec.SchemaNode) bool {
			if seen[s.Site] {
				return false
			}
			seen[s.Site] = true
			if s.Property == "" {
				return true
			}
			n := s.Node
			if spec.Ref(n) != "" {
				return false // did not resolve; its type is in a file legible could not read
			}
			typed := len(spec.Types(n)) > 0 || composed(n) || spec.Get(n, "enum") != nil ||
				spec.Get(n, "const") != nil || spec.Get(n, "properties") != nil
			ctx.CheckWeighted(typed, nestedWeight(s.Depth), op.Label(), s.Site, s.Pointer,
				fmt.Sprintf("request property %q has no type: a model will send whatever seems plausible", s.Property))
			return true
		})
	}
}

func polymorphismDiscriminator(ctx *engine.Context) {
	seen := map[*yaml.Node]bool{}
	for _, op := range ctx.Doc.Operations() {
		schema, ptr, _ := ctx.Doc.RequestSchema(op)
		ctx.Doc.Walk(schema, ptr, func(s spec.SchemaNode) bool {
			if seen[s.Node] {
				return false
			}
			seen[s.Node] = true
			for _, kw := range []string{"oneOf", "anyOf"} {
				if members := objectVariants(ctx.Doc, spec.Get(s.Node, kw)); members > 1 {
					ctx.Check(spec.Get(s.Node, "discriminator") != nil, op.Label(), s.Site, s.Pointer,
						fmt.Sprintf("%s of %d schemas with no discriminator: a model has to infer which variant it is building", kw, members))
				}
			}
			return true
		})
	}
}

// objectVariants counts the union members that are object shapes. Only a
// choice between objects needs a discriminator. {type: null} is how OpenAPI
// 3.1 spells "nullable", and a scalar alternative is picked by the value
// itself: Stripe's `anyOf: [string, enum [""]]`, meaning "a value, or empty to
// clear it", has nothing to discriminate. Counting those flagged every one of
// Stripe's 1,158 unions.
func objectVariants(doc *spec.Document, list *yaml.Node) int {
	n := 0
	for _, m := range spec.Items(list) {
		r, _ := doc.Resolve(m)
		if spec.HasType(r, "object") || spec.Get(r, "properties") != nil || spec.Get(r, "allOf") != nil {
			n++
		}
	}
	return n
}
