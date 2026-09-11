package spec

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

var epoch = time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)

// clock pins now for the test, and returns a setter.
func clock(t *testing.T) func(time.Duration) {
	t.Helper()
	at := epoch
	now = func() time.Time { return at }
	t.Cleanup(func() { now = time.Now })
	return func(d time.Duration) { at = epoch.Add(d) }
}

func TestFreshnessFromHeaders(t *testing.T) {
	for name, c := range map[string]struct {
		headers map[string]string
		expires time.Duration
		store   bool
	}{
		"no-store":                {map[string]string{"Cache-Control": "no-store"}, 0, false},
		"max-age":                 {map[string]string{"Cache-Control": "public, max-age=60", "ETag": `"a"`}, time.Minute, true},
		"no-cache beats max-age":  {map[string]string{"Cache-Control": "max-age=60, no-cache"}, 0, true},
		"expires":                 {map[string]string{"Expires": epoch.Add(10 * time.Minute).Format(http.TimeFormat)}, 10 * time.Minute, true},
		"unparseable expires":     {map[string]string{"Expires": "soon"}, 0, true},
		"validators only":         {map[string]string{"ETag": `"a"`}, 0, true},
		"last-modified only":      {map[string]string{"Last-Modified": epoch.Format(http.TimeFormat)}, 0, true},
		"nothing at all":          {map[string]string{}, DefaultFreshness, true},
		"max-age beats validator": {map[string]string{"Cache-Control": "max-age=120", "Last-Modified": epoch.Format(http.TimeFormat)}, 2 * time.Minute, true},
	} {
		h := http.Header{}
		for k, v := range c.headers {
			h.Set(k, v)
		}
		expires, store := freshness(h, epoch)
		if store != c.store || (store && !expires.Equal(epoch.Add(c.expires))) {
			t.Errorf("%s: expires +%v store %v; want +%v %v", name, expires.Sub(epoch), store, c.expires, c.store)
		}
	}
}

// headerOrigin serves one file with the given headers, on 304s as well, and
// honours If-None-Match when it sends an ETag.
func headerOrigin(t *testing.T, headers map[string]string) (url string, requests *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		if etag := headers["ETag"]; etag != "" && r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = w.Write([]byte("Thing: {type: object}\n"))
	}))
	t.Cleanup(s.Close)
	return s.URL + "/common.yaml", &n
}

func resolveAt(t *testing.T, url string, opts Options) {
	t.Helper()
	d, err := ParseWith("root.yaml", []byte("openapi: 3.1.0\nx: {$ref: '"+url+"#/Thing'}\n"), opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Target(d.Pointer("#/x"), Ref(d.Pointer("#/x"))); err != nil {
		t.Fatal(err)
	}
}

// Each case runs once at the start, then again at quiet (expecting no new
// request) and at due (expecting exactly one).
func TestFreshCopiesAreUsedWithoutAsking(t *testing.T) {
	for name, c := range map[string]struct {
		headers   map[string]string
		quiet     time.Duration
		due       time.Duration
		noRequest bool // quiet is not checked when nothing is ever fresh
	}{
		"max-age":        {map[string]string{"Cache-Control": "max-age=600", "ETag": `"v1"`}, 5 * time.Minute, 11 * time.Minute, false},
		"no validators":  {map[string]string{}, 30 * time.Minute, 61 * time.Minute, false},
		"validator only": {map[string]string{"ETag": `"v1"`}, 0, time.Second, true},
		"no-cache":       {map[string]string{"Cache-Control": "no-cache", "ETag": `"v1"`}, 0, time.Second, true},
	} {
		t.Run(name, func(t *testing.T) {
			set := clock(t)
			url, n := headerOrigin(t, c.headers)
			opts := Options{Cache: DirCache{Dir: t.TempDir()}}
			resolveAt(t, url, opts)
			if !c.noRequest {
				set(c.quiet)
				resolveAt(t, url, opts)
				if n.Load() != 1 {
					t.Errorf("at +%v: %d requests, want the fresh copy used without one", c.quiet, n.Load())
				}
			}
			set(c.due)
			resolveAt(t, url, opts)
			if n.Load() != 2 {
				t.Errorf("at +%v: %d requests, want 2", c.due, n.Load())
			}
		})
	}
}

func TestNoStoreIsNotCached(t *testing.T) {
	clock(t)
	url, _ := headerOrigin(t, map[string]string{"Cache-Control": "no-store"})
	cache := DirCache{Dir: t.TempDir()}
	resolveAt(t, url, Options{Cache: cache})
	if _, _, ok := cache.Get(url); ok {
		t.Error("a no-store response was cached")
	}
}

func TestRefreshAsksEvenWhenFresh(t *testing.T) {
	set := clock(t)
	url, n := headerOrigin(t, map[string]string{"Cache-Control": "max-age=600", "ETag": `"v1"`})
	cache := DirCache{Dir: t.TempDir()}
	resolveAt(t, url, Options{Cache: cache})
	set(time.Minute)
	resolveAt(t, url, Options{Cache: cache, Refresh: true})
	if n.Load() != 2 {
		t.Errorf("%d requests, want --refresh to revalidate a fresh copy", n.Load())
	}
}

// A 304 renews freshness from its own headers: revalidated at +11m with
// max-age=600, the copy is fresh again until +21m.
func TestNotModifiedRenewsFreshness(t *testing.T) {
	set := clock(t)
	url, n := headerOrigin(t, map[string]string{"Cache-Control": "max-age=600", "ETag": `"v1"`})
	opts := Options{Cache: DirCache{Dir: t.TempDir()}}
	resolveAt(t, url, opts)
	set(11 * time.Minute)
	resolveAt(t, url, opts) // 304
	set(15 * time.Minute)
	resolveAt(t, url, opts)
	if n.Load() != 2 {
		t.Errorf("%d requests; the 304 at +11m should have renewed the copy until +21m", n.Load())
	}
}
