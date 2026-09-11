package rules

import (
	"errors"
	"fmt"

	"github.com/JustSteveKing/legible/engine"
	"github.com/JustSteveKing/legible/spec"
)

// unresolvedRef checks every $ref an agent could reach. One that does not
// resolve is not a style problem: the schema behind it is simply missing,
// and every other rule has been quietly checking around the hole.
//
// A remote file that could not be fetched for a reason that is not the
// spec's fault (offline, network down, a 5xx, credentials) is not counted
// either way. It is reported as skipped instead, and the report is marked
// incomplete. Only a 404 or 410 from the server makes a remote ref a finding.
func unresolvedRef(ctx *engine.Context) {
	ctx.Doc.Refs(func(u spec.RefUse) {
		if errors.Is(u.Err, spec.ErrNotFetched) {
			return
		}
		ctx.Check(u.Err == nil, "", u.Node, "", fmt.Sprintf("$ref %q does not resolve: %v", u.Ref, u.Err))
	})
}
