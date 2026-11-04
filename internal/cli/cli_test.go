package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/papilio-platform/Node/internal/buildinfo"
	"github.com/papilio-platform/Node/internal/cli"
)

func TestRunUsage(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		code   int
		stdout string
		stderr string
	}{
		{name: "no command", code: 2, stderr: "Usage:"},
		{name: "help", args: []string{"help"}, stdout: "Usage:"},
		{name: "short help", args: []string{"-h"}, stdout: "Usage:"},
		{name: "long help", args: []string{"--help"}, stdout: "Usage:"},
		{name: "extra help argument", args: []string{"help", "unexpected"}, code: 2, stderr: "does not accept"},
		{name: "unknown command", args: []string{"serve"}, code: 2, stderr: "unknown command"},
		{name: "unknown root flag", args: []string{"--unknown"}, code: 2, stderr: "flag provided but not defined"},
		{name: "unknown version flag", args: []string{"version", "--unknown"}, code: 2, stderr: "flag provided but not defined"},
		{name: "extra version argument", args: []string{"version", "unexpected"}, code: 2, stderr: "positional arguments"},
		{name: "invalid JSON boolean", args: []string{"version", "--json=invalid"}, code: 2, stderr: "invalid boolean"},
		{name: "version help", args: []string{"version", "--help"}, stderr: "Usage of node version"},
		{name: "terminal help", args: []string{"tui", "--help"}, stderr: "Usage: node"},
		{name: "terminal pipe", args: []string{"tui"}, code: 2, stderr: "requires terminal input/output"},
		{name: "terminal argument", args: []string{"tui", "unexpected"}, code: 2, stderr: "positional arguments"},
		{name: "terminal flag", args: []string{"tui", "--unknown"}, code: 2, stderr: "flag provided but not defined"},
		{name: "invalid color", args: []string{"--color=rainbow", "help"}, code: 2, stderr: "color must be"},
		{name: "missing color", args: []string{"--color"}, code: 2, stderr: "flag needs an argument"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := cli.Run(context.Background(), tc.args, strings.NewReader(""), &stdout, &stderr, buildinfo.Info{})
			if code != tc.code {
				t.Fatalf("exit code = %d, want %d; stderr: %s", code, tc.code, &stderr)
			}
			for stream, expectation := range map[string]struct{ got, want string }{
				"stdout": {stdout.String(), tc.stdout},
				"stderr": {stderr.String(), tc.stderr},
			} {
				if expectation.want == "" && expectation.got != "" {
					t.Errorf("unexpected %s: %q", stream, expectation.got)
				} else if !strings.Contains(expectation.got, expectation.want) {
					t.Errorf("%s = %q, want to contain %q", stream, expectation.got, expectation.want)
				}
			}
		})
	}
}

func TestColorAndMachineOutput(t *testing.T) {
	for _, noColor := range []string{"", "1", "false", "0", "arbitrary"} {
		t.Run("NO_COLOR="+noColor, func(t *testing.T) {
			t.Setenv("NO_COLOR", noColor)
			t.Setenv("CLICOLOR_FORCE", "1")
			for _, color := range []string{"auto", "always", "never"} {
				for _, command := range [][]string{{"help"}, {"version"}, {"version", "--json"}} {
					args := append([]string{"--color=" + color}, command...)
					var stdout, stderr bytes.Buffer
					if code := cli.Run(context.Background(), args, strings.NewReader(""), &stdout, &stderr, buildinfo.Info{Version: "devel"}); code != 0 {
						t.Fatalf("%v: exit %d: %s", args, code, &stderr)
					}
					jsonOutput := len(command) == 2
					wantANSI := color == "always" && !jsonOutput
					if strings.Contains(stdout.String(), "\x1b") != wantANSI {
						t.Errorf("%v: unexpected styling: %q", args, &stdout)
					}
					if jsonOutput && !json.Valid(stdout.Bytes()) {
						t.Errorf("%v: invalid JSON: %q", args, &stdout)
					}
				}
			}
		})
	}
}

func TestRunVersion(t *testing.T) {
	info := buildinfo.Info{
		Version: "devel", Revision: "example-revision", Modified: true,
		GoVersion: "go1.26.0", OS: "linux", Arch: "arm64",
	}
	var stdout, stderr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"version"}, strings.NewReader(""), &stdout, &stderr, info); code != 0 {
		t.Fatalf("exit code = %d; stderr: %s", code, &stderr)
	}
	if got, want := stdout.String(), "PapilioVPN Node devel (go1.26.0 linux/arm64)\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Errorf("unexpected stderr: %s", &stderr)
	}
}

func TestRunVersionJSON(t *testing.T) {
	for _, revision := range []string{"example-revision", ""} {
		t.Run(revision, func(t *testing.T) {
			info := buildinfo.Info{
				Version: "devel", Revision: revision, Modified: false,
				GoVersion: "go1.26.0", OS: "linux", Arch: "amd64",
			}
			var stdout, stderr bytes.Buffer
			if code := cli.Run(context.Background(), []string{"version", "--json"}, strings.NewReader(""), &stdout, &stderr, info); code != 0 {
				t.Fatalf("exit code = %d; stderr: %s", code, &stderr)
			}
			var output map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
				t.Fatal(err)
			}
			want := map[string]any{
				"version": info.Version, "revision": info.Revision, "modified": false,
				"go_version": info.GoVersion, "os": info.OS, "arch": info.Arch,
			}
			if len(output) != len(want) {
				t.Fatalf("unexpected JSON contract: %s", &stdout)
			}
			for key, value := range want {
				if got, ok := output[key]; !ok || got != value {
					t.Errorf("%s = %#v (present %v), want %#v", key, got, ok, value)
				}
			}
			if stderr.Len() != 0 {
				t.Errorf("unexpected stderr: %s", &stderr)
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("output unavailable")
}

func TestRunOutputFailure(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"version"}, {"version", "--json"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stderr bytes.Buffer
			if code := cli.Run(context.Background(), args, strings.NewReader(""), failingWriter{}, &stderr, buildinfo.Info{}); code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if !strings.Contains(stderr.String(), "output unavailable") {
				t.Errorf("missing output error: %s", &stderr)
			}
		})
	}
}
