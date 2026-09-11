package spec

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"go.yaml.in/yaml/v3"
)

func loadSplit(t *testing.T) *Document {
	t.Helper()
	d, err := Load("testdata/split/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func op(t *testing.T, d *Document, id string) Operation {
	t.Helper()
	for _, o := range d.Operations() {
		if o.ID == id {
			return o
		}
	}
	t.Fatalf("no operation %q", id)
	return Operation{}
}

func TestRefsResolveRelativeToTheFileTheyAreIn(t *testing.T) {
	d := loadSplit(t)
	schema, _, _ := d.RequestSchema(op(t, d, "createPet"))
	pet, chain := d.Resolve(schema)
	if Get(Get(pet, "properties"), "name") == nil || len(chain) != 1 {
		t.Fatalf("createPet's body did not resolve to Pet: chain %v", chain)
	}
	if got := d.File(pet); got != "testdata/split/schemas/pet.yaml" {
		t.Errorf("File(Pet) = %q", got)
	}
	// "owner.yaml", written in schemas/pet.yaml, means schemas/owner.yaml.
	owner, _ := d.Resolve(Get(Get(pet, "properties"), "owner"))
	if Get(Get(owner, "properties"), "id") == nil {
		t.Error("owner.yaml was not resolved against schemas/")
	}
	if d.File(d.Root) != "" {
		t.Error("the root document should report no file name")
	}
}

func TestComponentRefsLeadIntoOtherFiles(t *testing.T) {
	d := loadSplit(t)
	var got []string
	o := op(t, d, "createPet")
	Pairs(Get(o.Node, "responses"), func(code string, _, raw *yaml.Node) {
		resp, chain := d.Resolve(raw)
		got = append(got, code+" "+Str(resp, "description")+" "+strings.Join(chain, " → "))
	})
	want := "422 failed #/components/responses/Problem → responses.yaml#/Problem"
	if len(got) != 1 || got[0] != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWalkFindsRecursionAcrossFiles(t *testing.T) {
	d := loadSplit(t)
	schema, ptr, _ := d.RequestSchema(op(t, d, "createPet"))
	loops := d.Walk(schema, ptr, func(SchemaNode) bool { return true })
	if len(loops) != 1 || !strings.HasSuffix(loops[0].Pointer, "/owner/properties/pets/items") {
		t.Errorf("loops = %+v", loops)
	}
}

func TestRefsReportsWhatDoesNotResolve(t *testing.T) {
	d := loadSplit(t)
	var bad []string
	n := 0
	d.Refs(func(u RefUse) {
		n++
		if u.Err != nil {
			bad = append(bad, u.Ref+": "+u.Err.Error())
		}
	})
	slices.Sort(bad)
	if len(bad) != 2 ||
		!strings.Contains(bad[0], "schemas/pet.yaml has nothing at #/NotHere") ||
		!strings.HasPrefix(bad[1], "nowhere.yaml#/X: ") {
		t.Errorf("unresolved = %q", bad)
	}
	// Every ref in the fixture is visited once: 4 in the root, 3 in
	// schemas/pet.yaml, 1 in schemas/owner.yaml. responses.yaml has none.
	if n != 8 {
		t.Errorf("visited %d refs, want 8", n)
	}
}

// server serves a small set of files and counts the requests it gets.
func server(t *testing.T, files map[string]string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		body, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s, &hits
}

const remoteRoot = `openapi: 3.1.0
paths:
  /a:
    get:
      operationId: a
      requestBody: {content: {application/json: {schema: {$ref: "schemas.yaml#/Thing"}}}}
`

func TestRemoteRootResolvesRelativeRefsAgainstItsURL(t *testing.T) {
	s, _ := server(t, map[string]string{
		"/api/openapi.yaml": remoteRoot,
		"/api/schemas.yaml": "Thing: {type: object, properties: {x: {type: string}}}\n",
	})
	d, err := Load(s.URL + "/api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	schema, _, _ := d.RequestSchema(d.Operations()[0])
	thing, _ := d.Resolve(schema)
	if Get(Get(thing, "properties"), "x") == nil {
		t.Fatal("the relative ref was not fetched from the root's URL")
	}
	if got := d.File(thing); got != s.URL+"/api/schemas.yaml" {
		t.Errorf("File = %q", got)
	}
}

func TestOfflineFetchesNothing(t *testing.T) {
	s, hits := server(t, map[string]string{"/common.yaml": "Thing: {type: object}\n"})
	src := "openapi: 3.1.0\nx: {$ref: '" + s.URL + "/common.yaml#/Thing'}\n"

	d, err := ParseWith("root.yaml", []byte(src), Options{Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Target(d.Pointer("#/x"), Ref(d.Pointer("#/x"))); !errors.Is(err, ErrOffline) {
		t.Errorf("err = %v, want ErrOffline", err)
	}
	if got := d.Skipped(); len(got) != 1 || got[0].URL != s.URL+"/common.yaml" || got[0].Reason != "offline, and not in the cache" {
		t.Errorf("Skipped = %+v", got)
	}
	if hits.Load() != 0 {
		t.Errorf("offline made %d requests", hits.Load())
	}
	if _, err := LoadWith(s.URL+"/common.yaml", Options{Offline: true}); !errors.Is(err, ErrOffline) {
		t.Errorf("an offline remote root: err = %v, want ErrOffline", err)
	}

	// Online, the same ref resolves, and a second use is served from cache.
	d, _ = Parse("root.yaml", []byte(src))
	for range 2 {
		if _, err := d.Target(d.Pointer("#/x"), Ref(d.Pointer("#/x"))); err != nil {
			t.Fatal(err)
		}
	}
	if hits.Load() != 1 {
		t.Errorf("online made %d requests, want 1", hits.Load())
	}
}

func TestRemoteFailuresSayWhy(t *testing.T) {
	s, _ := server(t, nil)
	d, _ := Parse("root.yaml", []byte("openapi: 3.1.0\nx: {$ref: '"+s.URL+"/gone.yaml'}\n"))
	_, err := d.Target(d.Pointer("#/x"), Ref(d.Pointer("#/x")))
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %v, want one naming the 404", err)
	}
}
