package integration_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/papilio-platform/Node/internal/cores/xray"
	"github.com/papilio-platform/Node/internal/instance"
)

func TestSS2022PreparedConfigurationRetainsSourceAndReusesHealthyExecution(t *testing.T) {
	source, _ := ss2022Source(t, accountServerPort(t), "2022-blake3-aes-128-gcm", `,"users":[]`)
	settings, life, root := nativeConfiguration(t, source)
	validation, err := settings.Validate(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatal("prepared validation retained runtime files", entries, err)
	}
	started, err := life.Start(t.Context())
	if err != nil || started.AppliedRevision != validation.Revision {
		t.Fatal("validation/start used different preparation", err)
	}
	view, err := settings.Read()
	if err != nil || !bytes.Equal(view.Source, source) || view.State.DesiredRevision != validation.Revision {
		t.Fatal("compiled input replaced original desired source", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatal("unexpected owned runtime directories", entries, err)
	}
	directory := filepath.Join(root, entries[0].Name())
	for path, permission := range map[string]os.FileMode{directory: 0700, filepath.Join(directory, "api.json"): 0600, filepath.Join(directory, "effective.pb"): 0600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != permission {
			t.Fatal("prepared secrets lack private permissions", err)
		}
	}
	reused, err := settings.Apply(t.Context(), started.DesiredRevision, source)
	if err != nil || !reused.Reused || reused.Current.Process.PID != started.Process.PID {
		t.Fatal("unchanged compiled execution was replaced", err)
	}
	stopped, err := life.Stop(t.Context())
	assertNativeShutdown(t, err)
	assertInstanceReleased(t, stopped)
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatal("compiled execution retained owned data", entries, err)
	}
}

func TestSS2022CompilationAndNativeValidationFailuresCleanUp(t *testing.T) {
	for _, failure := range []string{"compile", "native-validation", "managed-chacha"} {
		t.Run(failure, func(t *testing.T) {
			source, key := ss2022Source(t, accountServerPort(t), "2022-blake3-aes-128-gcm", `,"clients":[]`)
			switch failure {
			case "compile":
				source = bytes.Replace(source, []byte(`"protocol":"freedom"`), []byte(`"protocol":"papilio-unknown-outbound"`), 1)
			case "native-validation":
				source = bytes.Replace(source, []byte(key), []byte("bad-key"), 1)
				key = "bad-key"
			case "managed-chacha":
				source = bytes.Replace(source, []byte("2022-blake3-aes-128-gcm"), []byte("2022-blake3-chacha20-poly1305"), 1)
			}
			life, _, root := nativeInstance(t, source)
			state, err := life.Start(t.Context())
			if err == nil || state.Phase != instance.PhaseFailed || state.AppliedRevision != "" || state.Process.PID != 0 {
				t.Fatal("failed preparation claimed an applied execution", err)
			}
			if failure == "managed-chacha" {
				if !errors.Is(err, xray.ErrUnsupportedConfiguration) {
					t.Fatal("unavailable managed mode was hidden", err)
				}
			} else {
				var check *xray.CheckError
				var exit *exec.ExitError
				if !errors.As(err, &check) || !errors.As(err, &exit) || len(check.Diagnostics) == 0 || len(check.Diagnostics) > 4096 || strings.Contains(err.Error(), key) {
					t.Fatal("bounded native failure cause or privacy lost", err)
				}
				if err := syscall.Kill(exit.ProcessState.Pid(), 0); !errors.Is(err, syscall.ESRCH) {
					t.Fatal("preparation child was not joined", err)
				}
			}
			if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
				t.Fatal("failed preparation retained owned data", entries, err)
			}
		})
	}
}

func TestSS2022CancelledNativeConversionJoinsChildAndCleansFiles(t *testing.T) {
	fixture := t.TempDir()
	fifo := filepath.Join(fixture, "blocked-certificate.pem")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	source, _ := ss2022Source(t, accountServerPort(t), "2022-blake3-aes-128-gcm", `,"clients":[]`)
	stream := fmt.Sprintf(`"streamSettings":{"network":"raw","security":"tls","tlsSettings":{"certificates":[{"certificateFile":%q}]}},"settings":`, fifo)
	source = bytes.Replace(source, []byte(`"settings":`), []byte(stream), 1)
	launcher, _, root := nativeLauncher(t, source)
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	err := launcher.Validate(ctx)
	var check *xray.CheckError
	var exit *exec.ExitError
	if !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &check) || !errors.As(err, &exit) || !bytes.Contains(check.Diagnostics, []byte("Output ProtoBuf file")) {
		t.Fatal("blocked actual conversion did not respect cancellation", err)
	}
	if err := syscall.Kill(exit.ProcessState.Pid(), 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatal("cancelled converter still exists", err)
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatal("cancelled conversion retained private files", entries, err)
	}
}

func BenchmarkSS2022ManagedValidation(b *testing.B) {
	source, _ := ss2022Source(b, accountServerPort(b), "2022-blake3-aes-128-gcm", `,"clients":[]`)
	launcher, _, _ := nativeLauncher(b, source)
	b.ReportAllocs()
	for b.Loop() {
		if err := launcher.Validate(b.Context()); err != nil {
			b.Fatal(err)
		}
	}
}
