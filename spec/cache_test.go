package spec

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// origin serves one file with an ETag, honours If-None-Match, and can be
// switched to failing, so each branch of the cache can be driven.
type origin struct {
	*httptest.Server
	full, notModified atomic.Int32
	status            atomic.Int32 // 0 serves normally; anything else is returned as-is
}

func newOrigin(t *testing.T) *origin {
	t.Helper()
	o := &origin{}
	o.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s := o.status.Load(); s != 0 {
			w.WriteHeader(int(s))
			return
		}
		if r.Header.Get("If-None-Match") == `"v1"` {
			o.notModified.Add(1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		o.full.Add(1)
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte("Thing: {type: object, properties: {x: {type: string}}}\n"))
	}))
	t.Cleanup(o.Close)
	return o
}

// resolveThing parses a fresh root document, as a new run would, and
// resolves its one remote ref.
func resolveThing(t *testing.T, o *origin, opts Options) (*Document, error) {
	t.Helper()
	d, err := ParseWith("root.yaml", []byte("openapi: 3.1.0\nx: {$ref: '"+o.URL+"/common.yaml#/Thing'}\n"), opts)
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.Target(d.Pointer("#/x"), Ref(d.Pointer("#/x")))
	return d, err
}

func TestCacheAcrossRuns(t *testing.T) {
	o := newOrigin(t)
	cache := DirCache{Dir: t.TempDir()}
	opts := Options{Cache: cache}

	if _, err := resolveThing(t, o, opts); err != nil {
		t.Fatal(err)
	}
	if o.full.Load() != 1 {
		t.Fatalf("first run: %d downloads", o.full.Load())
	}
	if b, err := os.ReadFile(filepath.Join(cache.Dir, ".gitignore")); err != nil || !strings.Contains(string(b), "*") {
		t.Errorf("the cache did not ignore itself: %q, %v", b, err)
	}

	// A second run revalidates rather than downloading again.
	if _, err := resolveThing(t, o, opts); err != nil {
		t.Fatal(err)
	}
	if o.full.Load() != 1 || o.notModified.Load() != 1 {
		t.Errorf("second run: %d downloads, %d not-modified; want 1 and 1", o.full.Load(), o.notModified.Load())
	}

	// Offline, the cache answers and nothing is asked of the network.
	d, err := resolveThing(t, o, Options{Cache: cache, Offline: true})
	if err != nil || len(d.Skipped()) != 0 {
		t.Errorf("offline with a cached copy: err %v, skipped %v", err, d.Skipped())
	}
	if o.full.Load()+o.notModified.Load() != 2 {
		t.Error("offline made a request")
	}
}

func TestCacheCoversFailuresButNotDeletions(t *testing.T) {
	o := newOrigin(t)
	cache := DirCache{Dir: t.TempDir()}
	opts := Options{Cache: cache}
	if _, err := resolveThing(t, o, opts); err != nil {
		t.Fatal(err)
	}

	o.status.Store(http.StatusBadGateway)
	d, err := resolveThing(t, o, opts)
	if err != nil || len(d.Stale()) != 1 {
		t.Errorf("502 with a cached copy: err %v, stale %v; want the cached copy, marked stale", err, d.Stale())
	}

	// A file that has been deleted must not be hidden behind an old copy.
	o.status.Store(http.StatusNotFound)
	if _, err := resolveThing(t, o, opts); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("404 with a cached copy: err = %v, want the 404", err)
	}

	o.status.Store(0)
	o.Close() // the network is gone
	d, err = resolveThing(t, o, opts)
	if err != nil || len(d.Stale()) != 1 {
		t.Errorf("unreachable with a cached copy: err %v, stale %v", err, d.Stale())
	}
}

func TestCorruptCacheIsAMiss(t *testing.T) {
	cache := DirCache{Dir: t.TempDir()}
	const url = "https://example.com/common.yaml"
	if err := cache.Put(url, []byte("a: 1\n"), CacheInfo{ETag: `"x"`}); err != nil {
		t.Fatal(err)
	}
	if body, v, ok := cache.Get(url); !ok || string(body) != "a: 1\n" || v.ETag != `"x"` {
		t.Fatalf("round trip: %q %+v %v", body, v, ok)
	}
	body, _ := cache.paths(url)
	if err := os.WriteFile(body, []byte("a: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := cache.Get(url); ok {
		t.Error("a body that no longer matches its recorded hash was served")
	}
	if _, _, ok := cache.Get("https://example.com/other.yaml"); ok {
		t.Error("a URL never stored was served")
	}
}

func TestOfflineWithoutACachedCopyIsStillSkipped(t *testing.T) {
	o := newOrigin(t)
	d, err := resolveThing(t, o, Options{Cache: DirCache{Dir: t.TempDir()}, Offline: true})
	if !errors.Is(err, ErrOffline) || len(d.Skipped()) != 1 {
		t.Errorf("err %v, skipped %v", err, d.Skipped())
	}
}
