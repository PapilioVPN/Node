package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/papilio-platform/Node/internal/buildinfo"
	"github.com/papilio-platform/Node/internal/cli"
)

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
