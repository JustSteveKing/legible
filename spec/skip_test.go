package spec

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Only the server saying the file does not exist is the spec's fault. Every
// other way a fetch can fail is legible not getting the file, and is a skip:
// reported, marked, and not counted as a finding.
func TestWhatCountsAsSkipped(t *testing.T) {
	for status, skipped := range map[int]bool{
		http.StatusServiceUnavailable: true,
		http.StatusForbidden:          true,
		http.StatusUnauthorized:       true,
		http.StatusTooManyRequests:    true,
		http.StatusNotFound:           false,
		http.StatusGone:               false,
	} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
			defer s.Close()
			d, _ := Parse("root.yaml", []byte("openapi: 3.1.0\nx: {$ref: '"+s.URL+"/common.yaml'}\n"))
			_, err := d.Target(d.Pointer("#/x"), Ref(d.Pointer("#/x")))
			if err == nil {
				t.Fatal("resolved")
			}
			if errors.Is(err, ErrNotFetched) != skipped || (len(d.Skipped()) == 1) != skipped {
				t.Errorf("err %v, skipped %+v; want skipped = %v", err, d.Skipped(), skipped)
			}
			if skipped && !strings.Contains(d.Skipped()[0].Reason, fmt.Sprint(status)) {
				t.Errorf("reason %q does not name the status", d.Skipped()[0].Reason)
			}
		})
	}
}

func TestUnreachableIsSkipped(t *testing.T) {
	s := httptest.NewServer(http.NotFoundHandler())
	url := s.URL + "/common.yaml"
	s.Close()
	d, _ := Parse("root.yaml", []byte("openapi: 3.1.0\nx: {$ref: '"+url+"'}\n"))
	if _, err := d.Target(d.Pointer("#/x"), Ref(d.Pointer("#/x"))); !errors.Is(err, ErrNotFetched) {
		t.Errorf("err = %v, want ErrNotFetched", err)
	}
	if got := d.Skipped(); len(got) != 1 || got[0].URL != url || got[0].Reason == "" {
		t.Errorf("Skipped = %+v", got)
	}
}

// With a cached copy, anything short of a 404 falls back to it: a private
// host that now answers 403 still gets checked from the last good copy.
func TestCachedCopyCoversAForbiddenHost(t *testing.T) {
	o := newOrigin(t)
	opts := Options{Cache: DirCache{Dir: t.TempDir()}}
	if _, err := resolveThing(t, o, opts); err != nil {
		t.Fatal(err)
	}
	o.status.Store(http.StatusForbidden)
	d, err := resolveThing(t, o, opts)
	if err != nil || len(d.Stale()) != 1 || len(d.Skipped()) != 0 {
		t.Errorf("err %v, stale %v, skipped %v", err, d.Stale(), d.Skipped())
	}
}
