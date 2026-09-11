package rules

import (
	"github.com/JustSteveKing/legible/engine"
	"github.com/JustSteveKing/legible/spec"
)

// securityDeclared passes an operation that states its auth, either on
// itself or through the document's top-level security. An explicit empty list
// — `security: []` — passes too: it says "public" on purpose, which is
// exactly the information an agent needs.
func securityDeclared(ctx *engine.Context) {
	global := spec.Get(ctx.Doc.Root, "security") != nil
	for _, op := range ctx.Doc.Operations() {
		declared := global || spec.Get(op.Node, "security") != nil
		ctx.Check(declared, op.Label(), op.Node, op.Pointer,
			"no security requirement, here or at the top level: an agent cannot tell whether this needs credentials, or which")
	}
}
