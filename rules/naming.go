package rules

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/JustSteveKing/legible/engine"
	"github.com/JustSteveKing/legible/spec"
	"go.yaml.in/yaml/v3"
)

func operationID(ctx *engine.Context) {
	first := map[string]string{}
	for _, op := range ctx.Doc.Operations() {
		if op.ID == "" {
			ctx.Fail(op.Label(), op.Node, op.Pointer, "no operationId: tool generators invent a name from the method and path, and it changes whenever the path does")
			continue
		}
		if prev, ok := first[op.ID]; ok {
			ctx.Fail(op.Label(), spec.Get(op.Node, "operationId"), op.Pointer+"/operationId",
				fmt.Sprintf("operationId %q is also used by %s: two tools cannot share a name", op.ID, prev))
			continue
		}
		first[op.ID] = op.Label()
		ctx.Pass()
	}
}

// toolNamePattern is the name every major tool-calling API accepts. OpenAI
// allows ^[a-zA-Z0-9_-]{1,64}$ and Anthropic ^[a-zA-Z0-9_-]{1,128}$; the
// intersection is what a spec can rely on.
var toolNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func toolName(ctx *engine.Context) {
	for _, op := range ctx.Doc.Operations() {
		if op.ID == "" {
			continue // operation-id reports it
		}
		msg := fmt.Sprintf("operationId %q is not a valid tool name: use only letters, digits, _ and -", op.ID)
		if len(op.ID) > 64 {
			msg = fmt.Sprintf("operationId %q is %d characters: tool names are limited to 64", op.ID, len(op.ID))
		}
		ctx.Check(toolNamePattern.MatchString(op.ID), op.Label(), spec.Get(op.Node, "operationId"), op.Pointer+"/operationId", msg)
	}
}

type style string

const (
	camel     style = "camelCase"
	pascal    style = "PascalCase"
	snake     style = "snake_case"
	kebab     style = "kebab-case"
	screaming style = "SCREAMING_SNAKE_CASE"
)

// styleOf classifies a name, returning "" for names that fit several styles
// (a single lower-case word is valid camel, snake and kebab alike) or none.
func styleOf(name string) style {
	hasUpper := strings.IndexFunc(name, unicode.IsUpper) >= 0
	hasLower := strings.IndexFunc(name, unicode.IsLower) >= 0
	switch {
	case strings.Contains(name, "_") && strings.Contains(name, "-"):
		return ""
	case strings.Contains(name, "_"):
		if !hasLower {
			return screaming
		}
		if !hasUpper {
			return snake
		}
		return ""
	case strings.Contains(name, "-"):
		if !hasUpper {
			return kebab
		}
		return ""
	case hasUpper && hasLower:
		if unicode.IsUpper([]rune(name)[0]) {
			return pascal
		}
		return camel
	}
	return ""
}

type named struct {
	name, op, ptr string
	node          *yaml.Node
}

func namingStyle(ctx *engine.Context) {
	var ids, params, props []named
	seenParam := map[string]bool{}
	seenProp := map[string]bool{}
	// Only names matter here, so a schema walked for one operation never
	// needs walking again for the next. Without this every operation
	// re-walked Stripe's shared response graph: 9 seconds for one rule.
	seenNode := map[*yaml.Node]bool{}
	for _, op := range ctx.Doc.Operations() {
		if op.ID != "" {
			ids = append(ids, named{op.ID, op.Label(), op.Pointer + "/operationId", spec.Get(op.Node, "operationId")})
		}
		for _, p := range ctx.Doc.Parameters(op) {
			// Header names follow HTTP convention, not the API's.
			if p.In == "header" || p.In == "cookie" || seenParam[p.Name] {
				continue
			}
			seenParam[p.Name] = true
			params = append(params, named{p.Name, op.Label(), p.Pointer + "/name", spec.Get(p.Node, "name")})
		}
		collect := func(schema *yaml.Node, ptr string) {
			ctx.Doc.Walk(schema, ptr, func(s spec.SchemaNode) bool {
				if s.Property != "" && !seenProp[s.Property] {
					seenProp[s.Property] = true
					props = append(props, named{s.Property, op.Label(), s.Pointer, s.Site})
				}
				if seenNode[s.Node] {
					return false
				}
				seenNode[s.Node] = true
				return true
			})
		}
		schema, ptr, _ := ctx.Doc.RequestSchema(op)
		collect(schema, ptr)
		jsonResponses(ctx.Doc, op, func(code string, _, schema *yaml.Node, _, ptr string) {
			collect(schema, ptr)
		})
	}
	checkStyle(ctx, "operationId", ids)
	checkStyle(ctx, "parameter", params)
	checkStyle(ctx, "property", props)
}

// checkStyle finds the dominant style in a group and fails the names that
// depart from it. It does not prefer any style: an API that is consistently
// snake_case is exactly as legible as one that is consistently camelCase.
func checkStyle(ctx *engine.Context, kind string, names []named) {
	counts := map[style]int{}
	for _, n := range names {
		if s := styleOf(n.name); s != "" {
			counts[s]++
		}
	}
	var dominant style
	for _, s := range []style{camel, snake, kebab, pascal, screaming} {
		if counts[s] > counts[dominant] {
			dominant = s
		}
	}
	for _, n := range names {
		s := styleOf(n.name)
		if s == "" || dominant == "" {
			continue // fits every style, or there is nothing to compare with
		}
		ctx.Check(s == dominant, n.op, n.node, n.ptr,
			fmt.Sprintf("%s %q is %s where %d of this API's %s names are %s", kind, n.name, s, counts[dominant], kind, dominant))
	}
}
