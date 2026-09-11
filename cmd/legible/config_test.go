package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), ".legible.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

type jsonReport struct {
	Rules      []struct{ ID string }
	Findings   []struct{ Rule, Operation, File string }
	Suppressed []struct{ Rule, Operation string }
}

func decode(t *testing.T, out string) jsonReport {
	t.Helper()
	var r jsonReport
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("report does not parse: %v\n%s", err, out)
	}
	return r
}

// Petstore's only error is argument-collision on PUT /user/{username}.
// Accepting it in config must clear the default gate, and move the finding
// to suppressed rather than drop it.
func TestConfigIgnoreClearsTheGate(t *testing.T) {
	cfg := writeConfig(t, `
rules:
  property-description: off
ignore:
  - rule: argument-collision
    operations: ["PUT /user/*"]
    reason: The path and body username must match, and the server checks it.
`)
	out, stderr, err := run(t, "", "check", petstore, "-f", "json", "--config", cfg)
	if err != nil {
		t.Fatalf("err = %v; the only error was ignored", err)
	}
	r := decode(t, out)
	if len(r.Suppressed) != 1 || r.Suppressed[0].Operation != "PUT /user/{username}" {
		t.Errorf("suppressed = %+v", r.Suppressed)
	}
	for _, f := range r.Findings {
		if f.Rule == "argument-collision" || f.Rule == "property-description" {
			t.Errorf("finding %s should be gone", f.Rule)
		}
	}
	for _, rule := range r.Rules {
		if rule.ID == "property-description" {
			t.Error("a rule set to off still ran")
		}
	}
	if stderr != "" {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestFlagsOverrideConfig(t *testing.T) {
	cfg := writeConfig(t, "fail-on: none\n")
	if _, _, err := run(t, "", "check", petstore, "--config", cfg); err != nil {
		t.Errorf("fail-on none in config: err = %v", err)
	}
	if _, _, err := run(t, "", "check", petstore, "--config", cfg, "--fail-on", "error"); !errors.Is(err, errGate) {
		t.Errorf("--fail-on error over config: err = %v, want the gate", err)
	}
}

func TestBadConfigIsAFailureNotAGate(t *testing.T) {
	cfg := writeConfig(t, "fail-undr: 80\n")
	_, _, err := run(t, "", "check", good, "--config", cfg)
	if err == nil || errors.Is(err, errGate) || !strings.Contains(err.Error(), "fail-undr") {
		t.Errorf("err = %v, want a load error naming the key", err)
	}
	if _, _, err := run(t, "", "check", good, "--config", cfg, "--no-config"); err == nil {
		t.Error("--config and --no-config together should be refused")
	}
}

func TestStaleIgnoreIsReported(t *testing.T) {
	cfg := writeConfig(t, `
ignore:
  - {rule: tool-name, operations: ["GET /nowhere"], reason: long gone}
`)
	_, stderr, _ := run(t, "", "check", good, "--config", cfg)
	if !strings.Contains(stderr, "the ignore for tool-name matched nothing") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestSplitSpecFindingsNameTheirFile(t *testing.T) {
	out, _, _ := run(t, "", "check", "../../spec/testdata/split/openapi.yaml", "-f", "json", "--fail-on", "none", "--no-config")
	unresolved := 0
	for _, f := range decode(t, out).Findings {
		if f.Rule != "unresolved-ref" {
			continue
		}
		unresolved++
		if !strings.HasSuffix(filepath.ToSlash(f.File), "spec/testdata/split/schemas/pet.yaml") {
			t.Errorf("unresolved ref reported in %q, want schemas/pet.yaml", f.File)
		}
	}
	if unresolved != 2 {
		t.Errorf("%d unresolved refs, want 2", unresolved)
	}
}

// The cache lives beside the config file, and lets a later offline run check
// what an earlier online run fetched.
func TestCacheSitsBesideTheConfig(t *testing.T) {
	var hits atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("Thing: {type: object, properties: {x: {type: string}}}\n"))
	}))
	defer s.Close()
	cfg := writeConfig(t, "fail-on: none\n")
	root := filepath.Join(filepath.Dir(cfg), "openapi.yaml")
	body := `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /a:
    post:
      operationId: a
      requestBody: {content: {application/json: {schema: {$ref: "` + s.URL + `/common.yaml#/Thing"}}}}
`
	if err := os.WriteFile(root, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := run(t, "", "check", root, "--config", cfg); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(filepath.Dir(cfg), ".legible")
	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); err != nil {
		t.Fatalf("no .legible/.gitignore beside the config: %v", err)
	}

	out, stderr, _ := run(t, "", "check", root, "--config", cfg, "--offline", "-f", "json")
	if strings.Contains(stderr, "not checked") || hits.Load() != 1 {
		t.Errorf("offline after a cached run: %d requests, stderr %q", hits.Load(), stderr)
	}
	for _, f := range decode(t, out).Findings {
		if f.Rule == "unresolved-ref" {
			t.Error("the cached ref did not resolve offline")
		}
	}

	out, _, _ = run(t, "", "check", root, "--config", cfg, "--offline", "--no-cache")
	if !strings.Contains(out, "Not checked:") {
		t.Errorf("--no-cache still read the cache:\n%s", out)
	}
}

func TestOfflineFetchesNothingAndSaysSo(t *testing.T) {
	var hits atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("Thing: {type: object, properties: {x: {type: string}}}\n"))
	}))
	defer s.Close()
	root := filepath.Join(t.TempDir(), "openapi.yaml")
	spec := `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /a:
    post:
      operationId: a
      requestBody: {content: {application/json: {schema: {$ref: "` + s.URL + `/common.yaml#/Thing"}}}}
`
	if err := os.WriteFile(root, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}

	// --no-cache throughout: without it the online run would write a real
	// .legible into this repository, and the next run's offline check would
	// be served from it.
	out, stderr, _ := run(t, "", "check", root, "--offline", "--no-config", "--no-cache", "--fail-on", "none")
	if hits.Load() != 0 || !strings.Contains(out, "1 remote file could not be fetched") || !strings.Contains(out, "offline, and not in the cache") {
		t.Errorf("offline: %d requests, output:\n%s", hits.Load(), out)
	}
	if stderr != "" {
		t.Errorf("text output already says what was skipped, so stderr should be quiet: %q", stderr)
	}

	cfg := writeConfig(t, "allow-remote: false\n")
	if _, _, _ = run(t, "", "check", root, "--config", cfg, "--no-cache", "--fail-on", "none"); hits.Load() != 0 {
		t.Error("allow-remote: false in config still fetched")
	}

	_, stderr, _ = run(t, "", "check", root, "--no-config", "--no-cache", "--fail-on", "none")
	if hits.Load() != 1 || stderr != "" {
		t.Errorf("online: %d requests, stderr %q; want one fetch, since refs are followed by default", hits.Load(), stderr)
	}
}
