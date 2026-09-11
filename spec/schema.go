package spec

import (
	"slices"

	"go.yaml.in/yaml/v3"
)

// SchemaNode is one schema reached while walking.
type SchemaNode struct {
	// Node is the schema after $ref resolution.
	Node *yaml.Node
	// Site is the node where the schema appears, before resolution. Findings
	// point here: a problem with a shared component is reported where it is
	// used, which is where an agent meets it.
	Site *yaml.Node
	// Pointer locates Site in the document.
	Pointer string
	// Property is the property name when this schema is a property value.
	Property string
	// Depth counts object and array nesting from the walk's root, which is 0.
	// Composition keywords do not add depth: allOf of two flat objects is
	// still flat to whoever fills it in.
	Depth int
	// Refs is the chain of $refs followed from the root to reach this node.
	Refs []string
}

// Walk visits a schema and every schema nested in it, depth first, resolving
// local $refs as it goes. fn returning false stops descent below that node.
//
// fn is called at every site, but each resolved schema is descended into at
// most once per walk. A component reached through twenty properties is
// reported at all twenty, and its insides are walked once. Re-descending
// shared components is exponential on a heavily shared graph: Stripe's
// response schemas did not finish in five minutes that way, and take
// milliseconds this way. Depth is therefore the depth of the first route to
// a node, not the deepest; rules that need the maximum compute it themselves.
//
// Reaching a schema that is already on the current path is recursion. It is
// reported through recursive and not descended into, so walking always
// terminates and the caller learns exactly where the cycle closes.
//
// The path is tracked by resolved node, not by $ref string. Comparing strings
// only works when the walk enters the cycle through a $ref; starting at a
// component itself, the first lap goes unnoticed and the cycle is reported a
// level late. Nodes also make two spellings of one pointer the same schema.
func (d *Document) Walk(root *yaml.Node, ptr string, fn func(SchemaNode) bool) (recursive []SchemaNode) {
	visited := map[*yaml.Node]bool{}
	var visit func(site *yaml.Node, ptr, prop string, depth int, refs []string, path []*yaml.Node)
	visit = func(site *yaml.Node, ptr, prop string, depth int, refs []string, path []*yaml.Node) {
		if site == nil {
			return
		}
		n := site
		chain := refs
		for ref := Ref(n); ref != ""; ref = Ref(n) {
			target, err := d.Target(n, ref)
			if err != nil || target == n {
				break // unresolvable; the node holding the $ref is what we have
			}
			chain = append(slices.Clone(chain), ref)
			n = target
			if len(chain) > len(refs)+64 {
				break // a pure $ref loop with no schema in it
			}
		}
		if slices.Contains(path, n) {
			recursive = append(recursive, SchemaNode{Node: n, Site: site, Pointer: ptr, Property: prop, Depth: depth, Refs: chain})
			return
		}
		sn := SchemaNode{Node: n, Site: site, Pointer: ptr, Property: prop, Depth: depth, Refs: chain}
		if !fn(sn) || visited[n] {
			return
		}
		visited[n] = true
		path = append(slices.Clone(path), n)
		Pairs(Get(n, "properties"), func(name string, _, v *yaml.Node) {
			visit(v, Join(ptr+"/properties", name), name, depth+1, chain, path)
		})
		if items := Get(n, "items"); items != nil && items.Kind == yaml.MappingNode {
			visit(items, ptr+"/items", "", depth+1, chain, path)
		}
		if ap := Get(n, "additionalProperties"); ap != nil && ap.Kind == yaml.MappingNode {
			visit(ap, ptr+"/additionalProperties", "", depth+1, chain, path)
		}
		// Members of a composition are not properties, even when the
		// composition is a property's value: `owner: {anyOf: [A, B]}` is one
		// property, checked where it appears. Passing the name down made each
		// member look like the property again, which counted every union-typed
		// field in Stripe's spec two or three times.
		for _, kw := range []string{"allOf", "oneOf", "anyOf", "prefixItems"} {
			for i, s := range Items(Get(n, kw)) {
				visit(s, ptr+"/"+kw+"/"+itoa(i), "", depth, chain, path)
			}
		}
	}
	visit(root, ptr, "", 0, nil, nil)
	return recursive
}

// Types returns a schema's declared types. OpenAPI 3.0 allows one type as a
// string; 3.1 allows a list.
func Types(n *yaml.Node) []string {
	t := Get(n, "type")
	if t == nil {
		return nil
	}
	if t.Kind == yaml.ScalarNode {
		return []string{t.Value}
	}
	var out []string
	for _, v := range Items(t) {
		out = append(out, v.Value)
	}
	return out
}

// HasType reports whether a schema declares typ among its types.
func HasType(n *yaml.Node, typ string) bool { return slices.Contains(Types(n), typ) }
