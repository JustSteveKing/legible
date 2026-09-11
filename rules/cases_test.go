package rules

import (
	"fmt"
	"strings"
	"testing"

	"github.com/JustSteveKing/legible/engine"
	"github.com/JustSteveKing/legible/spec"
)

// ruleCase is one rule run over one small document. checked and failed are
// counted by hand from the YAML: checked is how many places the rule should
// look, failed how many of those it should flag. Asserting on both is what
// keeps the score honest, since a rule that silently stops counting its
// passes inflates it.
type ruleCase struct {
	name            string
	rule            string
	doc             string
	checked, failed int
}

const header = "openapi: 3.1.0\ninfo: {title: t, version: '1'}\n"

// nest builds a schema whose innermost property sits depth levels down.
func nest(depth int) string {
	if depth == 0 {
		return "{type: string}"
	}
	return "{type: object, properties: {p: " + nest(depth-1) + "}}"
}

func queryParams(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "        - {name: p%d, in: query, schema: {type: string}}\n", i)
	}
	return b.String()
}

var cases = []ruleCase{
	{"no description", "operation-description", `
paths:
  /pets:
    get: {operationId: listPets}
`, 1, 1},
	{"summary only", "operation-description", `
paths:
  /pets:
    get: {operationId: listPets, summary: List pets}
`, 1, 1},
	{"one sentence", "operation-description", `
paths:
  /pets:
    get: {operationId: listPets, description: Lists pets.}
`, 1, 1},
	{"two sentences", "operation-description", `
paths:
  /pets:
    get: {operationId: listPets, description: Lists every pet in the store. Use it to browse.}
`, 1, 0},

	{"restates, and says something", "description-restates-name", `
paths:
  /pets/{petId}:
    get: {operationId: getPet, description: Get a pet.}
    delete: {operationId: deletePet, description: Deletes a pet and its vaccination records.}
`, 2, 1},

	{"shared path parameter counted once", "parameter-description", `
paths:
  /pets/{petId}:
    parameters:
      - {name: petId, in: path, required: true, schema: {type: integer}}
    get: {operationId: getPet}
    delete: {operationId: deletePet}
  /pets:
    get:
      operationId: listPets
      parameters:
        - {name: limit, in: query, schema: {type: integer, description: Most to return.}}
`, 2, 1},

	{"shared component property counted once", "property-description", `
paths:
  /pets:
    post:
      operationId: createPet
      requestBody: {content: {application/json: {schema: {$ref: '#/components/schemas/Pet'}}}}
  /pets/{petId}:
    put:
      operationId: replacePet
      requestBody: {content: {application/json: {schema: {$ref: '#/components/schemas/Pet'}}}}
components:
  schemas:
    Pet:
      type: object
      properties:
        name: {type: string, description: The pet's name.}
        tag: {type: string}
`, 2, 1},

	// One property whose value is a union. Its members are not properties,
	// so this is one check, not three.
	{"a union-typed property counts once", "property-description", `
paths:
  /a:
    post:
      operationId: a
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                note:
                  anyOf:
                    - {type: string}
                    - {type: string, enum: [""]}
`, 1, 1},

	{"identical up to case and punctuation", "duplicate-description", `
paths:
  /a:
    get: {operationId: a, description: Only the logged in user can do this.}
  /b:
    get: {operationId: b, description: "only the logged-in user can do this"}
  /c:
    get: {operationId: c, description: Something else entirely.}
`, 3, 2},

	{"missing and duplicated", "operation-id", `
paths:
  /a:
    get: {operationId: listThings}
    post: {operationId: listThings}
  /b:
    get: {description: nothing}
`, 3, 2},

	{"dots and length", "tool-name", `
paths:
  /a:
    get: {operationId: pets.list}
    post: {operationId: listPets}
    put: {operationId: ` + strings.Repeat("a", 65) + `}
`, 3, 2},

	{"one snake among camels; headers and single words skipped", "naming-style", `
paths:
  /pets/{petId}:
    get:
      operationId: getPet
      parameters:
        - {name: petId, in: path, required: true, schema: {type: integer}}
        - {name: ownerId, in: query, schema: {type: integer}}
        - {name: sort_order, in: query, schema: {type: string}}
        - {name: limit, in: query, schema: {type: integer}}
        - {name: X-Trace-Id, in: header, schema: {type: string}}
`, 4, 1},

	{"success only, and default", "error-responses", `
paths:
  /a:
    get:
      operationId: a
      responses: {"200": {description: ok}}
    post:
      operationId: b
      responses: {default: {description: failed}}
`, 2, 1},

	{"bodiless 404, shared problem counted once", "error-schema", `
paths:
  /a:
    get:
      operationId: a
      responses:
        "404": {description: missing}
        "500": {$ref: '#/components/responses/Problem'}
    post:
      operationId: b
      responses:
        "500": {$ref: '#/components/responses/Problem'}
components:
  responses:
    Problem:
      description: failed
      content: {application/problem+json: {schema: {type: object, properties: {detail: {type: string}}}}}
`, 2, 1},

	{"same fields under another name, and an outlier", "error-shape", `
paths:
  /a:
    get:
      operationId: a
      responses:
        "400": {description: bad, content: {application/json: {schema: {$ref: '#/components/schemas/Problem'}}}}
        "404": {description: gone, content: {application/json: {schema: {type: object, properties: {title: {type: string}, detail: {type: string}}}}}}
        "500": {description: broke, content: {application/json: {schema: {type: object, properties: {message: {type: string}}}}}}
components:
  schemas:
    Problem: {type: object, properties: {detail: {type: string}, title: {type: string}}}
`, 3, 1},

	{"leaf examples all the way down, and none", "request-example", `
paths:
  /a:
    post:
      operationId: a
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                name: {type: string, example: Rex}
                owner:
                  type: object
                  properties:
                    id: {type: integer, example: 7}
    put:
      operationId: b
      requestBody:
        content:
          application/json:
            schema: {type: object, properties: {name: {type: string}}}
`, 2, 1},

	{"one leaf missing, no body, media example", "response-example", `
paths:
  /a:
    get:
      operationId: a
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {type: object, properties: {id: {type: integer, example: 1}, name: {type: string}}}
    delete:
      operationId: b
      responses:
        "204": {description: gone}
    post:
      operationId: c
      responses:
        "201":
          description: made
          content:
            application/json:
              schema: {type: object}
              example: {id: 1}
`, 2, 1},

	{"free-form, schemaless, typed, bodiless", "request-body-schema", `
paths:
  /a:
    post:
      operationId: a
      requestBody: {content: {application/json: {schema: {type: object}}}}
    put:
      operationId: b
      requestBody: {content: {application/json: {}}}
    patch:
      operationId: c
      requestBody: {content: {application/json: {schema: {type: object, properties: {name: {type: string}}}}}}
    get:
      operationId: d
`, 3, 2},

	// A body whose schema does not resolve is unresolved-ref's to report,
	// or was skipped. Flagging it as free-form would report one fault twice,
	// or blame the spec for legible's network.
	{"a schema that does not resolve is left alone", "request-body-schema", `
paths:
  /a:
    post:
      operationId: a
      requestBody: {content: {application/json: {schema: {$ref: '#/components/schemas/Missing'}}}}
`, 0, 0},

	{"an unresolved property is left alone", "untyped-property", `
paths:
  /a:
    post:
      operationId: a
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                a: {$ref: '#/components/schemas/Missing'}
                b: {type: string}
`, 1, 0},

	{"self-referencing tree", "recursive-schema", `
paths:
  /a:
    post:
      operationId: a
      requestBody: {content: {application/json: {schema: {$ref: '#/components/schemas/Node'}}}}
    put:
      operationId: b
      requestBody: {content: {application/json: {schema: {type: object, properties: {name: {type: string}}}}}}
components:
  schemas:
    Node:
      type: object
      properties:
        children: {type: array, items: {$ref: '#/components/schemas/Node'}}
`, 2, 1},

	{"username in path and body", "argument-collision", `
paths:
  /users/{username}:
    put:
      operationId: a
      parameters:
        - {name: username, in: path, required: true, schema: {type: string}}
      requestBody: {content: {application/json: {schema: {type: object, properties: {username: {type: string}, email: {type: string}}}}}}
    get:
      operationId: b
      parameters:
        - {name: username, in: path, required: true, schema: {type: string}}
`, 2, 1},

	{"six deep and five deep", "schema-depth", `
paths:
  /a:
    post:
      operationId: a
      requestBody: {content: {application/json: {schema: ` + nest(6) + `}}}
    put:
      operationId: b
      requestBody: {content: {application/json: {schema: ` + nest(5) + `}}}
`, 2, 1},

	{"sixteen and fifteen", "parameter-count", `
paths:
  /a:
    get:
      operationId: a
      parameters:
` + queryParams(16) + `
    post:
      operationId: b
      parameters:
` + queryParams(15), 2, 1},

	{"untyped, ref, nullable, enum, nested", "untyped-property", `
paths:
  /a:
    post:
      operationId: a
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                note: {description: anything}
                owner: {$ref: '#/components/schemas/Owner'}
                age: {type: [integer, "null"]}
                kind: {enum: [cat, dog]}
components:
  schemas:
    Owner: {type: object, properties: {id: {type: integer}}}
`, 5, 1},

	{"bare union, nullable, discriminated", "polymorphism-discriminator", `
paths:
  /a:
    post:
      operationId: a
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                pay:
                  oneOf:
                    - {$ref: '#/components/schemas/Card'}
                    - {$ref: '#/components/schemas/Bank'}
                nick:
                  anyOf:
                    - {type: string}
                    - {type: "null"}
                ship:
                  oneOf:
                    - {$ref: '#/components/schemas/Card'}
                    - {$ref: '#/components/schemas/Bank'}
                  discriminator: {propertyName: type}
components:
  schemas:
    Card: {type: object, properties: {type: {const: card}}}
    Bank: {type: object, properties: {type: {const: bank}}}
`, 2, 1},

	{"html descriptions count as their text", "operation-description", `
paths:
  /a:
    get: {operationId: a, description: "<p>Retrieves the balance.</p><p>Use it before a payout.</p>"}
    post: {operationId: b, description: "<p>Retrieves the balance.</p>"}
`, 2, 1},

	{"scalar unions need no discriminator", "polymorphism-discriminator", `
paths:
  /a:
    post:
      operationId: a
      requestBody:
        content:
          application/x-www-form-urlencoded:
            schema:
              type: object
              properties:
                description:
                  anyOf:
                    - {type: string}
                    - {type: string, enum: [""]}
`, 0, 0},

	// Deep is reached first at 1+4 = 5 levels, then at 2+4 = 6. Taking the
	// depth of whichever route the walk found first would pass this.
	{"a shared component's deepest route counts", "schema-depth", `
paths:
  /a:
    post:
      operationId: a
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                a: {$ref: '#/components/schemas/Deep'}
                b: {type: object, properties: {c: {$ref: '#/components/schemas/Deep'}}}
components:
  schemas:
    Deep: ` + nest(4) + `
`, 1, 1},

	{"a dangling pointer, and a good one", "unresolved-ref", `
paths:
  /a:
    post:
      operationId: a
      requestBody: {content: {application/json: {schema: {$ref: '#/components/schemas/Missing'}}}}
    put:
      operationId: b
      requestBody: {content: {application/json: {schema: {$ref: '#/components/schemas/Here'}}}}
components:
  schemas:
    Here: {type: object}
`, 2, 1},

	{"undeclared, and deliberately public", "security-declared", `
paths:
  /a:
    get: {operationId: a}
    post: {operationId: b, security: []}
`, 2, 1},
	{"covered by top-level security", "security-declared", `
security: [{key: []}]
paths:
  /a:
    get: {operationId: a}
`, 1, 0},
}

// TestOptionsChangeTheOutcome sets each declared option so that a case which
// fails at the default passes, which proves the rule reads the option rather
// than its constant.
func TestOptionsChangeTheOutcome(t *testing.T) {
	for _, c := range []struct {
		rule, option string
		value        int
		doc          string
	}{
		{"parameter-count", "max", 16, "paths:\n  /a:\n    get:\n      operationId: a\n      parameters:\n" + queryParams(16)},
		{"schema-depth", "max", 6, "paths:\n  /a:\n    post:\n      operationId: a\n      requestBody: {content: {application/json: {schema: " + nest(6) + "}}}\n"},
		{"operation-description", "min-sentences", 1, "paths:\n  /a:\n    get: {operationId: a, description: Lists pets.}\n"},
	} {
		doc, err := spec.Parse("case.yaml", []byte(header+c.doc))
		if err != nil {
			t.Fatal(err)
		}
		r := Find(c.rule)
		if engine.Run(doc, []*engine.Rule{r}).Rules[0].Failed != 1 {
			t.Errorf("%s: the case should fail at the default", c.rule)
		}
		r.Options[c.option] = c.value
		if res := engine.Run(doc, []*engine.Rule{r}).Rules[0]; res.Failed != 0 {
			t.Errorf("%s with %s=%d still failed: %s", c.rule, c.option, c.value, res.Findings[0].Message)
		}
	}
}

func TestRules(t *testing.T) {
	covered := map[string]bool{}
	for _, c := range cases {
		covered[c.rule] = true
		t.Run(c.rule+"/"+c.name, func(t *testing.T) {
			doc, err := spec.Parse("case.yaml", []byte(header+c.doc))
			if err != nil {
				t.Fatal(err)
			}
			r := Find(c.rule)
			if r == nil {
				t.Fatalf("no rule %q", c.rule)
			}
			res := engine.Run(doc, []*engine.Rule{r}).Rules[0]
			if res.Checked != c.checked || res.Failed != c.failed {
				t.Errorf("checked %d, failed %d; want checked %d, failed %d", res.Checked, res.Failed, c.checked, c.failed)
				for _, f := range res.Findings {
					t.Logf("  line %d: %s", f.Line, f.Message)
				}
			}
			for _, f := range res.Findings {
				if f.Line == 0 {
					t.Errorf("finding has no line number: %s", f.Message)
				}
			}
		})
	}
	for _, r := range All() {
		if !covered[r.ID] {
			t.Errorf("rule %q has no case in TestRules", r.ID)
		}
	}
}
