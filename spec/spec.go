// Package spec loads an OpenAPI 3.x document into a tree of YAML nodes that
// keeps every value's line and column, and resolves $refs on demand: within
// the document, to other files relative to the one the ref is written in,
// and to URLs.
//
// It is deliberately not a validator and not a typed model. Rules need three
// things a typed model makes hard: the position of whatever they complain
// about, the difference between a value that is absent and one that is empty,
// and control over $ref resolution so that recursion is something a rule can
// see rather than something the loader hides.
package spec

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Methods are the operation keys of a path item, in the order the OpenAPI
// specification lists them. Reports follow this order so output is stable.
var Methods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

// Options controls how a document and the files it refers to are loaded.
type Options struct {
	// Offline refuses http and https. A remote $ref is left unresolved and
	// listed by SkippedRemote; a remote root document fails to load.
	Offline bool
	// Client fetches remote files. nil means a client with a 30 second
	// timeout.
	Client *http.Client
	// Cache keeps remote files between runs. With one, a cached file is
	// revalidated with a conditional request, and the cached copy is used
	// when the run is Offline, when the network fails, or when the server
	// answers with anything but the file. The exception is a 404 or 410: a
	// file that has been deleted should not be hidden behind an old copy.
	// nil caches nothing.
	Cache Cache
	// Refresh revalidates every cached file now, ignoring how long its
	// headers said it would stay fresh.
	Refresh bool
}

// MaxRemoteBytes bounds a single fetched file. A spec is not a download
// manager, and a ref to the wrong URL should fail rather than fill memory.
const MaxRemoteBytes = 32 << 20

// ErrNotFetched is the root of every reason a remote file was not checked
// that is not the spec's fault: the network, the server, credentials, or
// running offline. A 404 is not one of them. The server saying the file does
// not exist is exactly what unresolved-ref is for.
var ErrNotFetched = errors.New("not fetched")

// ErrOffline is why a remote $ref was not followed under Offline with no
// cached copy.
var ErrOffline = fmt.Errorf("%w: offline, and not in the cache", ErrNotFetched)

// Skip is a remote file that could not be fetched, and why. The schemas
// behind it were not checked, so a report with any Skip is incomplete.
type Skip struct {
	URL    string `json:"url"`
	Reason string `json:"reason"`
}

// ErrSwagger is returned for Swagger 2.0 documents, which are out of scope.
var ErrSwagger = errors.New("swagger 2.0 is not supported; convert it to OpenAPI 3 first")

// Document is a parsed OpenAPI document, along with every other file its
// $refs have led to so far. Files are loaded when first referred to and kept
// for the life of the Document, which is not safe for concurrent use.
type Document struct {
	// Path is how the root document was named when loaded, used in reports.
	Path string
	// Version is the value of the top-level openapi field.
	Version string
	// Root is the root document's top-level mapping node.
	Root *yaml.Node

	opts  Options
	root  *file
	files map[string]*file
	// owner maps each node of every non-root file to its file. Root nodes
	// are left out: they are the bulk of any spec, and a miss means root.
	owner   map[*yaml.Node]*file
	skipped map[string]string // URL → reason
	stale   map[string]bool
}

// file is one loaded document: the root or anything a $ref reached.
type file struct {
	loc  string // absolute path or URL, and the key in Document.files
	name string // how reports refer to it
	root *yaml.Node
	err  error // why it could not be loaded; root is nil when set

	// pointers caches lookups. Resolving "#/components/schemas/X" scans the
	// schemas map key by key, and a large spec resolves the same few
	// thousand refs millions of times: Stripe has 1,454 schemas.
	pointers map[string]*yaml.Node
}

// Load reads and parses the document at src, a file path or an http(s) URL.
func Load(src string) (*Document, error) { return LoadWith(src, Options{}) }

// LoadWith is Load with options.
func LoadWith(src string, opts Options) (*Document, error) {
	loc, err := locate(src)
	if err != nil {
		return nil, err
	}
	data, stale, err := opts.fetch(loc)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", src, err)
	}
	d, err := parse(src, loc, data, opts)
	if err == nil && stale {
		d.stale[loc] = true
	}
	return d, err
}

// Parse parses data as an OpenAPI 3.x document. name is used in reports, and
// relative $refs resolve against its directory.
func Parse(name string, data []byte) (*Document, error) { return ParseWith(name, data, Options{}) }

// ParseWith is Parse with options.
func ParseWith(name string, data []byte, opts Options) (*Document, error) {
	loc, err := locate(name)
	if err != nil {
		loc = name
	}
	return parse(name, loc, data, opts)
}

func parse(name, loc string, data []byte, opts Options) (*Document, error) {
	root, err := decode(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: not an OpenAPI document: the top level is not a mapping", name)
	}
	if Get(root, "swagger") != nil {
		return nil, fmt.Errorf("%s: %w", name, ErrSwagger)
	}
	version := Str(root, "openapi")
	if !strings.HasPrefix(version, "3.") {
		return nil, fmt.Errorf("%s: not an OpenAPI 3.x document: openapi is %q", name, version)
	}
	f := &file{loc: loc, name: name, root: root}
	return &Document{
		Path: name, Version: version, Root: root,
		opts: opts, root: f,
		files:   map[string]*file{loc: f},
		owner:   map[*yaml.Node]*file{},
		skipped: map[string]string{},
		stale:   map[string]bool{},
	}, nil
}

func decode(data []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.NewDecoder(bytes.NewReader(data)).Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("empty document")
		}
		return nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, errors.New("empty document")
	}
	return doc.Content[0], nil
}

func isURL(s string) bool { return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") }

// locate turns a path into an absolute one; URLs are returned as they are.
func locate(src string) (string, error) {
	if isURL(src) {
		return src, nil
	}
	return filepath.Abs(src)
}

// fetch reads a file or URL. stale reports that a URL could not be fetched
// and a cached copy was used in its place.
func (o Options) fetch(loc string) (data []byte, stale bool, err error) {
	if !isURL(loc) {
		data, err := os.ReadFile(loc)
		return data, false, err
	}
	var cached []byte
	var v CacheInfo
	have := false
	if o.Cache != nil {
		cached, v, have = o.Cache.Get(loc)
	}
	if o.Offline {
		if have {
			return cached, false, nil
		}
		return nil, false, ErrOffline
	}
	if have && !o.Refresh && now().Before(v.Expires) {
		return cached, false, nil // still fresh: no need to ask
	}
	req, err := http.NewRequest(http.MethodGet, loc, nil)
	if err != nil {
		return nil, false, err
	}
	if have {
		if v.ETag != "" {
			req.Header.Set("If-None-Match", v.ETag)
		}
		if v.LastModified != "" {
			req.Header.Set("If-Modified-Since", v.LastModified)
		}
	}
	client := o.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		if have {
			return cached, true, nil // unreachable: the last good copy will do
		}
		return nil, false, fmt.Errorf("%w: %v", ErrNotFetched, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotModified && have:
		// A 304 renews freshness. It need not repeat the validators, and
		// judging it without them would mistake a revalidated file for one
		// that has none.
		h := resp.Header.Clone()
		if h.Get("ETag") == "" && h.Get("Last-Modified") == "" {
			h.Set("ETag", v.ETag)
			h.Set("Last-Modified", v.LastModified)
		}
		if expires, store := freshness(h, now()); store && o.Cache != nil {
			_ = o.Cache.Put(loc, cached, CacheInfo{ETag: h.Get("ETag"), LastModified: h.Get("Last-Modified"), Expires: expires})
		}
		return cached, false, nil
	case resp.StatusCode == http.StatusOK:
		data, err := io.ReadAll(io.LimitReader(resp.Body, MaxRemoteBytes+1))
		if err == nil && len(data) > MaxRemoteBytes {
			err = fmt.Errorf("fetching %s: larger than %d MB", loc, MaxRemoteBytes>>20)
		}
		if err != nil {
			if have {
				return cached, true, nil
			}
			return nil, false, fmt.Errorf("%w: %v", ErrNotFetched, err)
		}
		if expires, store := freshness(resp.Header, now()); store && o.Cache != nil {
			// Best effort: a cache that cannot be written costs the next
			// run a download, which is no reason to fail this one.
			_ = o.Cache.Put(loc, data, CacheInfo{
				ETag:         resp.Header.Get("ETag"),
				LastModified: resp.Header.Get("Last-Modified"),
				Expires:      expires,
			})
		}
		return data, false, nil
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		// The one answer that is the spec's fault: it refers to a file the
		// server says does not exist. Never covered by a cached copy.
		return nil, false, fmt.Errorf("fetching %s: %s", loc, resp.Status)
	case have:
		return cached, true, nil
	}
	// A 5xx, a 401 or 403 from a private host, a 429: the file may well be
	// there, legible just could not get it. Skipped, not failed.
	return nil, false, fmt.Errorf("%w: %s answered %s", ErrNotFetched, loc, resp.Status)
}

// Get returns the value for key in a mapping node, or nil when the node is
// not a mapping or the key is absent.
func Get(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

// Str returns the scalar value for key, or "" when it is absent or not a
// scalar.
func Str(n *yaml.Node, key string) string {
	v := Get(n, key)
	if v == nil || v.Kind != yaml.ScalarNode {
		return ""
	}
	return v.Value
}

// Pairs calls fn for each key and value of a mapping node, in document order.
func Pairs(n *yaml.Node, fn func(key string, keyNode, value *yaml.Node)) {
	if n == nil || n.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		fn(n.Content[i].Value, n.Content[i], n.Content[i+1])
	}
}

// Items returns the elements of a sequence node, or nil.
func Items(n *yaml.Node) []*yaml.Node {
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	return n.Content
}

// Ref returns the $ref of a node, or "".
func Ref(n *yaml.Node) string { return Str(n, "$ref") }

// Resolve follows a node's $ref chain and returns the target along with the
// refs it followed. It stops at the first ref it cannot resolve and returns
// the node holding that ref, so a caller sees where resolution ended rather
// than a nil. A ref cycle is returned the same way.
func (d *Document) Resolve(n *yaml.Node) (*yaml.Node, []string) {
	var chain []string
	seen := map[*yaml.Node]bool{}
	for n != nil {
		ref := Ref(n)
		if ref == "" || seen[n] {
			return n, chain
		}
		seen[n] = true
		target, err := d.Target(n, ref)
		if err != nil {
			return n, chain
		}
		chain = append(chain, ref)
		n = target
	}
	return n, chain
}

// Target resolves ref as it is written in the node from. A fragment-only ref
// ("#/components/...") resolves in from's own file, which is not necessarily
// the root: a pointer written in schemas/pet.yaml means schemas/pet.yaml. A
// relative path resolves against the directory of from's file, and a URL is
// fetched unless the document was loaded Offline. The error says why a ref
// did not resolve.
func (d *Document) Target(from *yaml.Node, ref string) (*yaml.Node, error) {
	f := d.fileOf(from)
	path, frag, _ := strings.Cut(ref, "#")
	if path != "" {
		loc, err := join(f.loc, path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", ref, err)
		}
		f = d.open(loc)
		if f.err != nil {
			return nil, f.err
		}
	}
	n := f.pointer(frag)
	if n == nil {
		if frag == "" {
			frag = "/"
		}
		return nil, fmt.Errorf("%s has nothing at #%s", f.name, frag)
	}
	return n, nil
}

// Pointer resolves a JSON pointer such as "#/components/schemas/Pet" in the
// root document. It returns nil for anything else.
func (d *Document) Pointer(ref string) *yaml.Node {
	if !strings.HasPrefix(ref, "#") {
		return nil
	}
	return d.root.pointer(strings.TrimPrefix(ref, "#"))
}

// File returns the name of the file n was read from, or "" when it is the
// root document.
func (d *Document) File(n *yaml.Node) string {
	if f := d.fileOf(n); f != d.root {
		return f.name
	}
	return ""
}

// Skipped lists the remote files that could not be fetched, for a reason that
// is not the spec's fault, and had no cached copy to stand in. Nothing behind
// them was checked.
func (d *Document) Skipped() []Skip {
	var out []Skip
	for loc, why := range d.skipped {
		out = append(out, Skip{URL: loc, Reason: why})
	}
	slices.SortFunc(out, func(a, b Skip) int { return strings.Compare(a.URL, b.URL) })
	return out
}

// Stale lists the URLs that could not be fetched this run and were served
// from the cache instead.
func (d *Document) Stale() []string {
	var out []string
	for loc := range d.stale {
		out = append(out, loc)
	}
	slices.Sort(out)
	return out
}

// RefUse is one $ref as written, and whether it resolved.
type RefUse struct {
	Node *yaml.Node // the mapping holding the $ref
	Ref  string
	Err  error // nil when it resolved
}

// Refs calls fn for every $ref reachable from the root document: each one
// written in it, and each one in the parts of other files those lead to.
// Parts of other files that nothing refers to are not visited, since nothing
// an agent sees comes from them.
func (d *Document) Refs(fn func(RefUse)) {
	seen := map[*yaml.Node]bool{}
	stack := []*yaml.Node{d.Root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil || seen[n] {
			continue
		}
		seen[n] = true
		if ref := Ref(n); ref != "" {
			target, err := d.Target(n, ref)
			fn(RefUse{Node: n, Ref: ref, Err: err})
			if err == nil {
				stack = append(stack, target)
			}
		}
		for i := len(n.Content) - 1; i >= 0; i-- {
			stack = append(stack, n.Content[i])
		}
	}
}

func (d *Document) fileOf(n *yaml.Node) *file {
	if f, ok := d.owner[n]; ok {
		return f
	}
	return d.root
}

// open returns the file at loc, loading it the first time. A file that fails
// to load is cached with its error, so a bad ref costs one attempt per run.
func (d *Document) open(loc string) *file {
	if f, ok := d.files[loc]; ok {
		return f
	}
	f := &file{loc: loc, name: display(loc)}
	d.files[loc] = f
	data, stale, err := d.opts.fetch(loc)
	if errors.Is(err, ErrNotFetched) {
		d.skipped[loc] = strings.TrimPrefix(err.Error(), ErrNotFetched.Error()+": ")
	}
	if stale {
		d.stale[loc] = true
	}
	if err == nil {
		f.root, err = decode(data)
	}
	if err != nil {
		f.err = fmt.Errorf("%s: %w", f.name, err)
		return f
	}
	stack := []*yaml.Node{f.root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		d.owner[n] = f
		stack = append(stack, n.Content...)
	}
	return f
}

// join resolves a ref's path part against the location of the file it is
// written in.
func join(base, rel string) (string, error) {
	if isURL(rel) {
		return rel, nil
	}
	if isURL(base) {
		b, err := url.Parse(base)
		if err != nil {
			return "", err
		}
		r, err := url.Parse(rel)
		if err != nil {
			return "", err
		}
		return b.ResolveReference(r).String(), nil
	}
	// A $ref is a URI reference, so a space in a file name arrives as %20.
	p, err := url.PathUnescape(rel)
	if err != nil {
		return "", err
	}
	p = filepath.FromSlash(p)
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	return filepath.Join(filepath.Dir(base), p), nil
}

// display names a file for reports: relative to the working directory when
// it is beneath it, which is what CI annotations expect, and as-is otherwise.
func display(loc string) string {
	if isURL(loc) {
		return loc
	}
	if wd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(wd, loc); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(loc)
}

// pointer resolves a JSON pointer fragment, without the leading "#", in this
// file. An empty fragment is the whole file.
func (f *file) pointer(frag string) *yaml.Node {
	if n, ok := f.pointers[frag]; ok {
		return n
	}
	n := lookup(f.root, frag)
	if f.pointers == nil {
		f.pointers = map[string]*yaml.Node{}
	}
	f.pointers[frag] = n
	return n
}

func lookup(n *yaml.Node, frag string) *yaml.Node {
	frag, err := url.PathUnescape(frag)
	if err != nil || n == nil {
		return nil
	}
	if frag == "" || frag == "/" {
		return n
	}
	for _, tok := range strings.Split(strings.TrimPrefix(frag, "/"), "/") {
		tok = strings.NewReplacer("~1", "/", "~0", "~").Replace(tok)
		switch n.Kind {
		case yaml.MappingNode:
			n = Get(n, tok)
		case yaml.SequenceNode:
			var i int
			if _, err := fmt.Sscanf(tok, "%d", &i); err != nil || i < 0 || i >= len(n.Content) {
				return nil
			}
			n = n.Content[i]
		default:
			return nil
		}
		if n == nil {
			return nil
		}
	}
	return n
}
