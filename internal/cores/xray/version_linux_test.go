package xray_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/papilio-platform/Node/internal/cores/xray"
)

func aliasExecutable(t *testing.T, options *xray.LaunchOptions, name string) {
	t.Helper()
	alias := filepath.Join(t.TempDir(), name)
	if err := os.Symlink(options.Check.Executable, alias); err != nil {
		t.Fatal(err)
	}
	options.Check.Executable = alias
}

func TestVersionFailuresDoNotCreateSession(t *testing.T) {
	for _, name := range []string{"version-malformed", "version-overflow", "version-exit", "version-block"} {
		t.Run(name, func(t *testing.T) {
			options := launchOptions(t)
			aliasExecutable(t, &options, name)
			if name == "version-block" {
				options.Check.Timeout = 100 * time.Millisecond
			}
			launcher, err := xray.NewLauncher(context.Background(), options, []byte(`{}`))
			if err != nil {
				t.Fatal(err)
			}
			session, err := launcher.Start(context.Background())
			if err == nil || session != nil {
				t.Fatal("version failure created a session")
			}
			if name == "version-block" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("version deadline lost: %v", err)
			}
			if name == "version-exit" {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 17 || strings.Contains(err.Error(), "private-version") {
					t.Fatalf("exit meaning or diagnostic privacy lost: %v", err)
				}
			}
			assertRuntimeEmpty(t, options.RuntimeDir)
		})
	}
}

func TestSessionVersionComesFromSelectedExecutable(t *testing.T) {
	options := launchOptions(t)
	options.ReadyTimeout = 5 * time.Second
	aliasExecutable(t, &options, "version-alternate")
	lifetime, cancel := context.WithCancel(context.Background())
	defer cancel()
	launcher, err := xray.NewLauncher(lifetime, options, helperConfig(t, helperRequest{Mode: "api-with-default-signal"}))
	if err != nil {
		t.Fatal(err)
	}
	session, err := launcher.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	defer session.Stop(ctx)
	if observation := session.Observe(); observation.Version != "25.3.6" || observation.VersionObservedAt.IsZero() {
		t.Fatalf("linked client version substituted for executable: %+v", observation)
	}
}

func TestRevisionIdentifiesCapturedInputWithoutRuntimePaths(t *testing.T) {
	input := []byte(`{"outbounds":[{"protocol":"freedom"}]}`)
	first, err := xray.NewLauncher(context.Background(), launchOptions(t), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := xray.NewLauncher(context.Background(), launchOptions(t), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Revision()) != 64 || first.Revision() != second.Revision() {
		t.Fatal("ephemeral execution settings changed the configuration revision")
	}
	input[0] = ' '
	third, err := xray.NewLauncher(context.Background(), launchOptions(t), input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision() == third.Revision() || first.Revision() != second.Revision() {
		t.Fatal("revision did not capture original bytes immutably")
	}
}
