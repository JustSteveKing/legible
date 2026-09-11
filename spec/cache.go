package spec

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Cache keeps fetched remote files between runs, so a spec whose $refs point
// at URLs is not downloaded afresh every time, and can still be checked when
// the network is not there.
type Cache interface {
	// Get returns a cached body and what is known about its freshness.
	Get(url string) (body []byte, info CacheInfo, ok bool)
	// Put stores a body.
	Put(url string, body []byte, info CacheInfo) error
}

// CacheInfo is what a cached copy was served with: the validators that let
// it be revalidated with a conditional request, and when it stops being
// fresh enough to use without asking at all.
type CacheInfo struct {
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"last_modified,omitempty"`
	Expires      time.Time `json:"expires"`
}

// DefaultFreshness is how long a copy fetched without validators or caching
// headers is used before it is downloaded again. With no ETag or
// Last-Modified there is no cheap way to ask whether a file has changed, so
// without this every run would download it in full. pricepaid's download
// cache ran into exactly that with the ONS geography portal. An hour covers
// an editing session; --refresh overrides it.
const DefaultFreshness = time.Hour

// now is swapped by tests.
var now = time.Now

// freshness decides from a response's headers when it stops being fresh and
// whether it may be stored at all. It follows the parts of RFC 9111 that
// matter to a file fetched once per run:
//
//   - no-store: do not keep it
//   - no-cache: keep it, but revalidate every time
//   - max-age, then Expires: fresh until then; an Expires that does not
//     parse means already expired
//   - validators but no caching headers: revalidate every time, which is
//     cheap because the answer is usually 304
//   - neither: DefaultFreshness
func freshness(h http.Header, fetched time.Time) (expires time.Time, store bool) {
	directives := map[string]string{}
	for _, d := range strings.Split(h.Get("Cache-Control"), ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(strings.ToLower(d)), "=")
		if k != "" {
			directives[k] = strings.Trim(v, `"`)
		}
	}
	if _, ok := directives["no-store"]; ok {
		return time.Time{}, false
	}
	if _, ok := directives["no-cache"]; ok {
		return fetched, true
	}
	if v, ok := directives["max-age"]; ok {
		if s, err := strconv.Atoi(v); err == nil && s >= 0 {
			return fetched.Add(time.Duration(s) * time.Second), true
		}
	}
	if e := h.Get("Expires"); e != "" {
		if t, err := http.ParseTime(e); err == nil {
			return t, true
		}
		return fetched, true
	}
	if h.Get("ETag") == "" && h.Get("Last-Modified") == "" {
		return fetched.Add(DefaultFreshness), true
	}
	return fetched, true
}

// DirCache is a Cache in a directory. Each URL gets a body file and a
// metadata file under Dir/cache, named by the SHA-256 of the URL.
type DirCache struct{ Dir string }

type cacheMeta struct {
	URL string `json:"url"`
	CacheInfo
	SHA256  string    `json:"sha256"`
	Fetched time.Time `json:"fetched"`
}

func (c DirCache) paths(url string) (body, meta string) {
	sum := sha256.Sum256([]byte(url))
	name := hex.EncodeToString(sum[:])
	dir := filepath.Join(c.Dir, "cache")
	return filepath.Join(dir, name+".body"), filepath.Join(dir, name+".json")
}

// Get checks the body against the hash recorded with it. A body and metadata
// that disagree, as when a run is killed between writing the two, is a miss,
// not a corrupt spec.
func (c DirCache) Get(url string) ([]byte, CacheInfo, bool) {
	bp, mp := c.paths(url)
	raw, err := os.ReadFile(mp)
	if err != nil {
		return nil, CacheInfo{}, false
	}
	var m cacheMeta
	if json.Unmarshal(raw, &m) != nil || m.URL != url {
		return nil, CacheInfo{}, false
	}
	body, err := os.ReadFile(bp)
	if err != nil {
		return nil, CacheInfo{}, false
	}
	if sum := sha256.Sum256(body); hex.EncodeToString(sum[:]) != m.SHA256 {
		return nil, CacheInfo{}, false
	}
	return body, m.CacheInfo, true
}

// Put writes the body before the metadata that vouches for it, each through
// a temporary file and a rename, so a reader never sees half a file.
func (c DirCache) Put(url string, body []byte, info CacheInfo) error {
	if err := c.init(); err != nil {
		return err
	}
	bp, mp := c.paths(url)
	sum := sha256.Sum256(body)
	meta, err := json.MarshalIndent(cacheMeta{url, info, hex.EncodeToString(sum[:]), now().UTC()}, "", "  ")
	if err != nil {
		return err
	}
	if err := writeAtomic(bp, body); err != nil {
		return err
	}
	return writeAtomic(mp, meta)
}

// init creates the directory with a .gitignore that ignores everything in
// it, so the cache stays out of version control without anyone having to
// edit their own .gitignore.
func (c DirCache) init() error {
	if err := os.MkdirAll(filepath.Join(c.Dir, "cache"), 0o755); err != nil {
		return err
	}
	gi := filepath.Join(c.Dir, ".gitignore")
	if _, err := os.Stat(gi); errors.Is(err, fs.ErrNotExist) {
		return os.WriteFile(gi, []byte("# Created by legible. Everything in here is a cache.\n*\n"), 0o644)
	}
	return nil
}

func writeAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
