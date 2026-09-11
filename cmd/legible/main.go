// Command legible scores an OpenAPI document for how well LLM agents can use
// it, and explains every finding.
package main

import (
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

// version is stamped at build time with -ldflags "-X main.version=...", as
// the release builds and the Makefile do.
var version = "dev"

// go install applies no ldflags, so a binary installed with
// `go install github.com/JustSteveKing/legible/cmd/legible@v0.1.0` reported
// "dev". The go command records the module version in the binary, so fall
// back to that. Test binaries and plain local builds have none, or
// "(devel)", and stay "dev".
func init() {
	if version != "dev" {
		return
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = strings.TrimPrefix(info.Main.Version, "v")
	}
}

// Exit codes. A gate failing and legible failing are different outcomes, and
// CI needs to tell them apart: one means fix the spec, the other means fix
// the pipeline.
const (
	exitOK    = 0
	exitGate  = 1
	exitError = 2
)

// errGate is returned when the spec loaded and was checked, but did not meet
// --fail-on or --fail-under. The report has already been written.
var errGate = errors.New("gate failed")

func main() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		if errors.Is(err, errGate) {
			os.Exit(exitGate)
		}
		fmt.Fprintln(os.Stderr, "legible:", err)
		os.Exit(exitError)
	}
	os.Exit(exitOK)
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "legible",
		Short: "Score an OpenAPI spec for how well LLM agents can use it",
		Long: `legible reads an OpenAPI 3.x document and reports what will trip up an
LLM or agent calling it as a set of tools: vague descriptions, inconsistent
naming, unhelpful errors, missing examples, and schemas that are hard to
turn into tool definitions.

Every rule has a guide. Run 'legible explain <rule>' for why it matters and
how to fix it, or 'legible explain' for the overview.`,
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newCheckCmd(), newRulesCmd(), newExplainCmd())
	return root
}
