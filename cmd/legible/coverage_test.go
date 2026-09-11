package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The split fixture's two unresolved refs are both in schemas/pet.yaml, a
// shared file no operation owns. A files ignore reaches them. The pattern
// starts with ** because the config sits in a temporary directory, not
// beside the fixture.
func TestIgnoreFindingsInASharedFile(t *testing.T) {
	cfg := writeConfig(t, `
ignore:
  - rule: unresolved-ref
    files: ["**/split/schemas/pet.yaml"]
    reason: The broken refs are in a file vendored from upstream.
`)
	out, _, _ := run(t, "", "check", "../../spec/testdata/split/openapi.yaml", "-f", "json", "--config", cfg, "--fail-on", "none")
	r := decode(t, out)
	suppressed := 0
	for _, s := range r.Suppressed {
		if s.Rule == "unresolved-ref" {
			suppressed++
		}
	}
	for _, f := range r.Findings {
		if f.Rule == "unresolved-ref" {
			t.Errorf("unresolved-ref still reported in %s", f.File)
		}
	}
	if suppressed != 2 {
		t.Errorf("%d unresolved refs suppressed, want 2", suppressed)
	}
}

// unreachableSpec writes a spec whose only request body is a $ref to a
// server that has already gone away.
func unreachableSpec(t *testing.T) string {
	t.Helper()
	s := httptest.NewServer(http.NotFoundHandler())
	url := s.URL + "/common.yaml"
	s.Close()
	root := filepath.Join(t.TempDir(), "openapi.yaml")
	body := `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /a:
    post:
      operationId: a
      requestBody: {content: {application/json: {schema: {$ref: "` + url + `#/Thing"}}}}
`
	if err := os.WriteFile(root, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// --fail-on-skipped turns an incomplete check into a failed gate: exit 1,
// with a reason on stderr, since the report itself shows no finding for it.
// The flag overrides config in both directions.
func TestFailOnSkipped(t *testing.T) {
	root := unreachableSpec(t)
	base := []string{"check", root, "--no-cache", "--fail-on", "none"}

	if _, _, err := run(t, "", append(base, "--no-config")...); err != nil {
		t.Errorf("without the flag: err = %v, want a pass", err)
	}
	_, stderr, err := run(t, "", append(base, "--no-config", "--fail-on-skipped")...)
	if !errors.Is(err, errGate) || !strings.Contains(stderr, "fail-on-skipped: 1 remote file(s) could not be fetched") {
		t.Errorf("with the flag: err = %v, stderr %q", err, stderr)
	}

	cfg := writeConfig(t, "fail-on-skipped: true\n")
	if _, _, err := run(t, "", append(base, "--config", cfg)...); !errors.Is(err, errGate) {
		t.Errorf("from config: err = %v, want the gate", err)
	}
	if _, _, err := run(t, "", append(base, "--config", cfg, "--fail-on-skipped=false")...); err != nil {
		t.Errorf("--fail-on-skipped=false over config: err = %v, want a pass", err)
	}

	// A complete check never trips it.
	if _, _, err := run(t, "", "check", good, "--no-config", "--fail-on-skipped"); err != nil {
		t.Errorf("complete spec: err = %v", err)
	}
}

// A ref that cannot be fetched is not the spec's fault. It must not be a
// finding, and the report must say it is incomplete, in the output and, for
// machine formats, on stderr.
func TestUnreachableRefIsSkippedNotFailed(t *testing.T) {
	s := httptest.NewServer(http.NotFoundHandler())
	url := s.URL + "/common.yaml"
	s.Close()
	root := filepath.Join(t.TempDir(), "openapi.yaml")
	body := `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /a:
    post:
      operationId: a
      requestBody: {content: {application/json: {schema: {$ref: "` + url + `#/Thing"}}}}
`
	if err := os.WriteFile(root, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out, stderr, err := run(t, "", "check", root, "-f", "json", "--no-config", "--no-cache")
	if err != nil {
		t.Errorf("err = %v; a skipped ref is not an error finding, so the default gate should pass", err)
	}
	var j struct {
		Complete bool
		Skipped  []struct{ URL, Reason string }
		Findings []struct{ Rule string }
	}
	if err := json.Unmarshal([]byte(out), &j); err != nil {
		t.Fatal(err)
	}
	if j.Complete || len(j.Skipped) != 1 || j.Skipped[0].URL != url {
		t.Errorf("complete %v, skipped %+v", j.Complete, j.Skipped)
	}
	for _, f := range j.Findings {
		if f.Rule == "unresolved-ref" {
			t.Error("an unreachable ref was counted as unresolved")
		}
	}
	if !strings.Contains(stderr, "not checked: "+url) {
		t.Errorf("json output, so stderr should say what was skipped: %q", stderr)
	}

	out, _, _ = run(t, "", "check", root, "--no-config", "--no-cache")
	if !strings.Contains(out, "(incomplete: see below)") || !strings.Contains(out, "Not checked: 1 remote file") {
		t.Errorf("text output does not say it is incomplete:\n%s", out)
	}
}
