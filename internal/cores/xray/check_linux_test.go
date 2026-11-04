package xray_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestCheckerRejectsNonExecutableFile(t *testing.T) {
	options := checkerOptions(t)
	options.Executable = filepath.Join(t.TempDir(), "not-executable")
	if err := os.WriteFile(options.Executable, []byte("not a program"), 0600); err != nil {
		t.Fatal(err)
	}
	err := newChecker(t, options).Check(context.Background(), []byte(`{}`))
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("non-executable error = %v", err)
	}
}

func TestCheckerCancelsAndReapsStartedChild(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(strconv.FormatBool(deadline), func(t *testing.T) {
			options := checkerOptions(t)
			if deadline {
				options.Timeout = 2 * time.Second
			}
			ready := filepath.Join(options.WorkingDir, "ready")
			checker := newChecker(t, options)
			config := helperConfig(t, helperRequest{Mode: "block", ReadyFile: ready})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- checker.Check(ctx, config) }()
			// Always join the operation, including when a handshake assertion fails.
			joined := false
			defer func() {
				cancel()
				if !joined {
					<-done
				}
			}()
			var pid int
			until := time.Now().Add(time.Second)
			for time.Now().Before(until) {
				data, err := os.ReadFile(ready)
				if err == nil {
					pid, err = strconv.Atoi(string(data))
					if err == nil {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
			}
			if pid <= 0 {
				t.Fatal("validation child never acknowledged startup")
			}
			want := context.DeadlineExceeded
			if !deadline {
				want = context.Canceled
				cancel()
			}
			select {
			case err := <-done:
				joined = true
				if !errors.Is(err, want) {
					t.Fatalf("cancellation cause = %v, want %v", err, want)
				}
			case <-time.After(4 * time.Second):
				t.Fatal("validation child was not stopped within the cleanup bound")
			}
			if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
				t.Fatalf("validation child %d still exists or was not reaped: %v", pid, err)
			}
		})
	}
}
