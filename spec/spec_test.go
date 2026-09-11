package spec

import (
	"errors"
	"slices"
	"testing"
)

func mustParse(t *testing.T, src string) *Document {
	t.Helper()
	d, err := Parse("t.yaml", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestParseRejects(t *testing.T) {
	if _, err := Parse("s", []byte("swagger: '2.0'\n")); !errors.Is(err, ErrSwagger) {
		t.Errorf("swagger 2.0: err = %v, want ErrSwagger", err)
	}
	for name, src := range map[string]string{
		"not a mapping": "- a\n- b\n",
		"no version":    "info: {}\n",
		"openapi 4":     "openapi: 4.0.0\n",
		"broken yaml":   "openapi: [\n",
	} {
		if _, err := Parse(name, []byte(src)); err == nil {
			t.Errorf("%s: parsed without error", name)
		}
	}
}

func TestParsesJSON(t *testing.T) {
	d := mustParse(t, `{"openapi": "3.0.3", "info": {"title": "t", "version": "1"}, "paths": {"/a": {"get": {"operationId": "a"}}}}`)
	if ops := d.Operations(); len(ops) != 1 || ops[0].ID != "a" {
		t.Errorf("operations = %+v", ops)
	}
}

func TestPointerRoundTrip(t *testing.T) {
	d := mustParse(t, `openapi: 3.1.0
paths:
  /pets/{id}:
    get: {operationId: getPet}
  /a~b:
    get: {operationId: tilde}
`)
	for _, op := range d.Operations() {
		if got := d.Pointer("#" + op.Pointer); got != op.Node {
			t.Errorf("pointer %q does not resolve back to %s", op.Pointer, op.Label())
		}
	}
	if got := d.Operations()[0].Pointer; got != "/paths/~1pets~1{id}/get" {
		t.Errorf("pointer = %q", got)
	}
	if d.Pointer("other.yaml#/a") != nil || d.Pointer("#/nope") != nil {
		t.Error("external and dangling refs should resolve to nil")
	}
}

func TestResolveStopsOnACycle(t *testing.T) {
	d := mustParse(t, `openapi: 3.1.0
components:
  schemas:
    A: {$ref: '#/components/schemas/B'}
    B: {$ref: '#/components/schemas/A'}
`)
	n, chain := d.Resolve(d.Pointer("#/components/schemas/A"))
	if n == nil || len(chain) != 2 {
		t.Errorf("chain = %v, node = %v", chain, n)
	}
}

func TestWalkReportsRecursionAndTerminates(t *testing.T) {
	d := mustParse(t, `openapi: 3.1.0
components:
  schemas:
    Node:
      type: object
      properties:
        name: {type: string}
        children: {type: array, items: {$ref: '#/components/schemas/Node'}}
`)
	var props []string
	loops := d.Walk(d.Pointer("#/components/schemas/Node"), "/root", func(s SchemaNode) bool {
		if s.Property != "" {
			props = append(props, s.Property)
		}
		return true
	})
	if len(loops) != 1 || loops[0].Pointer != "/root/properties/children/items" {
		t.Errorf("loops = %+v", loops)
	}
	if !slices.Equal(props, []string{"name", "children"}) {
		t.Errorf("visited properties = %v", props)
	}
}

func TestWalkDepthIgnoresComposition(t *testing.T) {
	d := mustParse(t, `openapi: 3.1.0
x:
  allOf:
    - {type: object, properties: {a: {type: string}}}
    - {type: object, properties: {b: {type: object, properties: {c: {type: string}}}}}
`)
	depth := map[string]int{}
	d.Walk(d.Pointer("#/x"), "", func(s SchemaNode) bool {
		if s.Property != "" {
			depth[s.Property] = s.Depth
		}
		return true
	})
	if depth["a"] != 1 || depth["b"] != 1 || depth["c"] != 2 {
		t.Errorf("depths = %v", depth)
	}
}

func TestOperationParametersOverridePathParameters(t *testing.T) {
	d := mustParse(t, `openapi: 3.1.0
paths:
  /a:
    parameters:
      - {name: limit, in: query, description: from the path}
      - {name: limit, in: header, description: a different parameter}
    get:
      parameters:
        - {name: limit, in: query, description: from the operation}
`)
	ps := d.Parameters(d.Operations()[0])
	if len(ps) != 2 {
		t.Fatalf("got %d parameters, want 2", len(ps))
	}
	if Str(ps[0].Node, "description") != "from the operation" || ps[1].In != "header" {
		t.Errorf("parameters = %s, %s", Str(ps[0].Node, "description"), ps[1].In)
	}
}

func TestJSONMediaPrefersJSONAndKeepsTheRealKey(t *testing.T) {
	d := mustParse(t, `openapi: 3.1.0
c:
  text/plain: {x: plain}
  application/problem+json: {x: problem}
  application/json; charset=utf-8: {x: json}
`)
	n, ptr := JSONMedia(d.Pointer("#/c"), "/c")
	if Str(n, "x") != "json" {
		t.Errorf("picked %q", Str(n, "x"))
	}
	if d.Pointer("#"+ptr) != n {
		t.Errorf("pointer %q does not name the key in the document", ptr)
	}
	d2 := mustParse(t, "openapi: 3.1.0\nc: {text/plain: {x: plain}, application/problem+json: {x: problem}}\n")
	if n, _ := JSONMedia(d2.Pointer("#/c"), "/c"); Str(n, "x") != "problem" {
		t.Errorf("without application/json, picked %q, want the +json type", Str(n, "x"))
	}
}

func TestTypes(t *testing.T) {
	d := mustParse(t, "openapi: 3.1.0\na: {type: string}\nb: {type: [integer, 'null']}\n")
	if !slices.Equal(Types(d.Pointer("#/a")), []string{"string"}) || !HasType(d.Pointer("#/b"), "null") {
		t.Error("types were not read from both the 3.0 and the 3.1 form")
	}
}
