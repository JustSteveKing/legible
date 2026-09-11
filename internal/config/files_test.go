package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/JustSteveKing/legible/engine"
)

func TestMatchPath(t *testing.T) {
	for _, c := range []struct {
		pattern, name string
		want          bool
	}{
		{"schemas/**", "schemas/pet.yaml", true},
		{"schemas/**", "schemas/deep/er/pet.yaml", true},
		{"schemas/**", "schemas", true}, // ** matches no segments too
		{"**/pet.yaml", "a/b/pet.yaml", true},
		{"a/**/c", "a/c", true},
		{"*.yaml", "a/b.yaml", false}, // * stays within a segment
		{"schemas/*.yaml", "schemas/pet.yaml", true},
		{"schemas/*.yaml", "other/pet.yaml", false},
		{"https://example.com/common/**", "https://example.com/common/v1/errors.yaml", true},
	} {
		if got := matchPath(c.pattern, c.name); got != c.want {
			t.Errorf("matchPath(%q, %q) = %v", c.pattern, c.name, got)
		}
	}
}

// Patterns are relative to the config file, so the findings below are given
// absolute paths under its directory, and the test's own working directory
// has nothing to do with the result.
func TestIgnoreByFile(t *testing.T) {
	dir := t.TempDir()
	c, err := Load(write(t, dir, ".legible.yaml", `
ignore:
  - {rule: unresolved-ref, files: ["schemas/**"], reason: Vendored from upstream.}
  - {rule: property-description, files: [openapi.yaml], operations: ["POST /pets"], reason: Both must match.}
  - {rule: error-shape, files: ["https://example.com/common/*.yaml"], reason: Shared remote errors.}
`))
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "openapi.yaml")
	ignore := c.Ignores(root)
	for _, tc := range []struct {
		name string
		f    engine.Finding
		want bool
	}{
		{"in a shared file", engine.Finding{Rule: "unresolved-ref", File: filepath.Join(dir, "schemas", "deep", "pet.yaml")}, true},
		{"root is not under schemas/", engine.Finding{Rule: "unresolved-ref"}, false},
		{"root file and operation", engine.Finding{Rule: "property-description", Operation: "POST /pets"}, true},
		{"root file, other operation", engine.Finding{Rule: "property-description", Operation: "GET /pets"}, false},
		{"operation, other file", engine.Finding{Rule: "property-description", Operation: "POST /pets", File: filepath.Join(dir, "schemas", "x.yaml")}, false},
		{"remote file", engine.Finding{Rule: "error-shape", File: "https://example.com/common/errors.yaml"}, true},
		{"other remote file", engine.Finding{Rule: "error-shape", File: "https://example.com/other/errors.yaml"}, false},
	} {
		if got := ignore(tc.f); got != tc.want {
			t.Errorf("%s: ignored = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestBadFilePattern(t *testing.T) {
	_, err := load(t, "ignore: [{rule: unresolved-ref, files: ['schemas/[a'], reason: x}]\n")
	if err == nil || !strings.Contains(err.Error(), "bad file pattern") {
		t.Errorf("err = %v", err)
	}
}
