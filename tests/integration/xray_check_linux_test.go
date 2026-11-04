package integration_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/papilio-platform/Node/internal/cores/xray"
)

func TestXrayConfigurationChecks(t *testing.T) {
	executable := os.Getenv("PAPILIO_TEST_XRAY")
	if executable == "" {
		t.Skip("set PAPILIO_TEST_XRAY to the pinned native Xray executable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	versionCmd := exec.CommandContext(ctx, executable, "version")
	versionCmd.Env = []string{}
	version, err := versionCmd.Output()
	if err != nil {
		t.Fatalf("read pinned Xray version: %v", err)
	}
	if !strings.Contains(string(version), "Xray 26.10.31 ") || !strings.Contains(string(version), " a4a3dff ") {
		t.Fatalf("unexpected test engine: %s", version)
	}

	work := t.TempDir()
	ambient := t.TempDir()
	invalid := []byte(`{"outbounds":[{"protocol":"papilio-invalid"}]}`)
	for _, dir := range []string{work, ambient} {
		if err := os.WriteFile(filepath.Join(dir, "config.json"), invalid, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"XRAY_LOCATION_CONFDIR", "xray.location.confdir"} {
		t.Setenv(name, ambient)
	}
	for _, name := range []string{"XRAY_LOCATION_CONFIG", "xray.location.config"} {
		t.Setenv(name, filepath.Join(ambient, "config.json"))
	}
	checker, err := xray.NewChecker(xray.CheckerOptions{
		Executable: executable, WorkingDir: work,
		MaxConfigBytes: 1 << 20, MaxDiagnosticBytes: 4096,
		Timeout: 5 * time.Second, WaitDelay: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Run("valid-without-ambient-config-or-listener", func(t *testing.T) {
		socket := filepath.Join(work, "api.sock")
		config := fmt.Appendf(nil, `{
			"log":{"loglevel":"none"},
			"api":{"tag":"api","listen":%q,"services":["StatsService"]},
			"stats":{},"outbounds":[{"tag":"direct","protocol":"freedom"}]
		}`, socket)
		if err := checker.Check(context.Background(), config); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(socket); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("configuration check created a listener: %v", err)
		}
	})
	for _, test := range []struct {
		name   string
		config []byte
	}{
		{"unknown-protocol", invalid},
		{"malformed-json", []byte(`{"outbounds":`)},
		{"empty-input", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := checker.Check(context.Background(), test.config)
			var failure *xray.CheckError
			var exit *exec.ExitError
			if !errors.As(err, &failure) || !errors.As(err, &exit) || exit.ExitCode() != 23 {
				t.Fatalf("expected native configuration exit 23, got %v", err)
			}
			if len(failure.Diagnostics) == 0 || len(failure.Diagnostics) > 4096 {
				t.Fatal("missing or unbounded native diagnostics")
			}
		})
	}
}
