package spec

import (
	"strings"

	"go.yaml.in/yaml/v3"
)

// Operation is one method on one path.
type Operation struct {
	Method   string // lower case, as it appears in the document
	Path     string
	ID       string // operationId, or ""
	Node     *yaml.Node
	PathItem *yaml.Node // resolved
	Pointer  string
}

// Label is how an operation is named in reports: "GET /pets/{id}".
func (o Operation) Label() string { return strings.ToUpper(o.Method) + " " + o.Path }

// Operations returns every operation in document order.
func (d *Document) Operations() []Operation {
	var ops []Operation
	Pairs(Get(d.Root, "paths"), func(path string, _, item *yaml.Node) {
		resolved, _ := d.Resolve(item)
		itemPtr := Join("/paths", path)
		for _, m := range Methods {
			n := Get(resolved, m)
			if n == nil || n.Kind != yaml.MappingNode {
				continue
			}
			ops = append(ops, Operation{
				Method:   m,
				Path:     path,
				ID:       Str(n, "operationId"),
				Node:     n,
				PathItem: resolved,
				Pointer:  Join(itemPtr, m),
			})
		}
	})
	return ops
}

// Param is a resolved parameter as it applies to one operation.
type Param struct {
	Name    string
	In      string
	Node    *yaml.Node // resolved
	Pointer string     // where it is declared
	Schema  *yaml.Node // resolved, or nil
}

// Parameters returns an operation's effective parameters: those declared on
// the path item, overridden by those on the operation with the same name and
// location, as OpenAPI specifies.
func (d *Document) Parameters(op Operation) []Param {
	var out []Param
	index := map[string]int{}
	add := func(list *yaml.Node, ptr string) {
		for i, raw := range Items(list) {
			n, _ := d.Resolve(raw)
			p := Param{
				Name:    Str(n, "name"),
				In:      Str(n, "in"),
				Node:    n,
				Pointer: Join(ptr, itoa(i)),
			}
			if s := Get(n, "schema"); s != nil {
				p.Schema, _ = d.Resolve(s)
			}
			key := p.In + "\x00" + p.Name
			if at, ok := index[key]; ok {
				out[at] = p
				continue
			}
			index[key] = len(out)
			out = append(out, p)
		}
	}
	itemPtr := strings.TrimSuffix(op.Pointer, "/"+op.Method)
	add(Get(op.PathItem, "parameters"), itemPtr+"/parameters")
	add(Get(op.Node, "parameters"), op.Pointer+"/parameters")
	return out
}

// RequestSchema returns the schema of an operation's JSON request body, the
// pointer to it, and whether the operation has a request body at all.
func (d *Document) RequestSchema(op Operation) (schema *yaml.Node, ptr string, hasBody bool) {
	body, _ := d.Resolve(Get(op.Node, "requestBody"))
	if body == nil {
		return nil, "", false
	}
	mt, mtPtr := JSONMedia(Get(body, "content"), op.Pointer+"/requestBody/content")
	if mt == nil {
		return nil, "", true
	}
	return Get(mt, "schema"), mtPtr + "/schema", true
}

// JSONMedia picks the JSON media type from a content map — application/json
// first, then any +json type, then the first entry — and returns it with its
// pointer. ptr is the pointer to the content map. It returns nil when the map
// is empty.
func JSONMedia(content *yaml.Node, ptr string) (*yaml.Node, string) {
	// Keys are kept as written — "application/json; charset=utf-8" included —
	// because the pointer has to name the key that is actually there.
	var exact, suffix, first *yaml.Node
	var exactKey, suffixKey, firstKey string
	Pairs(content, func(key string, _, v *yaml.Node) {
		mediaType, _, _ := strings.Cut(key, ";")
		mediaType = strings.TrimSpace(strings.ToLower(mediaType))
		switch {
		case mediaType == "application/json" && exact == nil:
			exact, exactKey = v, key
		case strings.HasSuffix(mediaType, "+json") && suffix == nil:
			suffix, suffixKey = v, key
		}
		if first == nil {
			first, firstKey = v, key
		}
	})
	switch {
	case exact != nil:
		return exact, Join(ptr, exactKey)
	case suffix != nil:
		return suffix, Join(ptr, suffixKey)
	case first != nil:
		return first, Join(ptr, firstKey)
	}
	return nil, ""
}

// Join appends one reference token to a JSON pointer, escaping it.
func Join(ptr, token string) string {
	return ptr + "/" + strings.NewReplacer("~", "~0", "/", "~1").Replace(token)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	n := len(b)
	for ; i > 0; i /= 10 {
		n--
		b[n] = byte('0' + i%10)
	}
	return string(b[n:])
}
