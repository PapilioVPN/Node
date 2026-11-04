package integration_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"os/signal"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/creack/pty"

	"github.com/papilio-platform/Node/internal/buildinfo"
	"github.com/papilio-platform/Node/internal/cli"
)

// The helper runs in its own session so signal and raw-terminal tests never
// change the test runner's terminal or signal handlers.
func TestTerminalHelper(t *testing.T) {
	if os.Getenv("PAPILIO_TERMINAL_TEST") != "1" {
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGUSR1)
	defer stop()
	os.Exit(cli.Run(ctx, []string{"tui"}, os.Stdin, os.Stdout, os.Stderr, buildinfo.Read()))
}

func TestTerminalSession(t *testing.T) {
	for _, exit := range []string{"quit", "ctrl+c", "sigterm", "cancel", "no-color"} {
		t.Run(exit, func(t *testing.T) {
			master, slave, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { master.Close() })
			t.Cleanup(func() { slave.Close() })
			before, err := term.GetState(slave.Fd())
			if err != nil {
				t.Fatal(err)
			}
			if err := pty.Setsize(slave, &pty.Winsize{Rows: 30, Cols: 96}); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(os.Args[0], "-test.run=^TestTerminalHelper$")
			command.Stdin, command.Stdout, command.Stderr = slave, slave, slave
			command.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
			// Append explicit values after inherited settings; os/exec retains the
			// last value for duplicate environment keys.
			command.Env = append(os.Environ(), "PAPILIO_TERMINAL_TEST=1", "TERM=xterm-256color", "COLORTERM=truecolor", "NO_COLOR=")
			if exit == "no-color" {
				command.Env = append(command.Env, "NO_COLOR=false")
			}
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			waitDone := make(chan struct{})
			go func() {
				defer close(waitDone)
				done <- command.Wait()
			}()
			readDone := make(chan struct{})
			changed := make(chan struct{}, 1)
			var lock sync.Mutex
			var output bytes.Buffer
			go func() {
				defer close(readDone)
				buffer := make([]byte, 4096)
				for {
					n, readErr := master.Read(buffer)
					lock.Lock()
					if output.Len() < 256*1024 {
						output.Write(buffer[:n])
					}
					lock.Unlock()
					select {
					case changed <- struct{}{}:
					default:
					}
					if readErr != nil {
						return
					}
				}
			}()
			t.Cleanup(func() {
				command.Process.Kill()
				select {
				case <-waitDone:
				case <-time.After(5 * time.Second):
					t.Error("terminal process did not stop")
				}
				slave.Close()
				master.Close()
				select {
				case <-readDone:
				case <-time.After(5 * time.Second):
					t.Error("terminal reader did not stop")
				}
			})
			waitFor := func(text string) {
				t.Helper()
				deadline := time.NewTimer(15 * time.Second)
				defer deadline.Stop()
				for {
					lock.Lock()
					got := output.String()
					lock.Unlock()
					if strings.Contains(got, text) {
						return
					}
					select {
					case <-changed:
					case <-deadline.C:
						t.Fatalf("terminal did not display %q; output: %q", text, got)
					}
				}
			}
			write := func(keys string) {
				t.Helper()
				if _, err := master.WriteString(keys); err != nil {
					t.Fatal(err)
				}
			}
			waitFor("YOUR NODE, IN FOCUS")
			write("\t")
			waitFor("BINARY IDENTITY")
			write("?")
			waitFor("KEYBOARD SHORTCUTS")
			if err := pty.Setsize(slave, &pty.Winsize{Rows: 10, Cols: 36}); err != nil {
				t.Fatal(err)
			}
			waitFor("Resize to at least 48 x 16.")
			switch exit {
			case "ctrl+c":
				write("\x03")
			case "sigterm":
				command.Process.Signal(syscall.SIGTERM)
			case "cancel":
				command.Process.Signal(syscall.SIGUSR1)
			default:
				write("q")
			}
			select {
			case err := <-done:
				if exit == "cancel" {
					if err == nil || command.ProcessState.ExitCode() != 1 {
						t.Errorf("cancellation was not reported: %v", err)
					}
				} else if err != nil {
					t.Errorf("console failed: %v", err)
				}
			case <-time.After(15 * time.Second):
				t.Fatal("console did not stop")
			}
			waitFor("\x1b[?1049l")
			waitFor("\x1b[?25h")
			after, err := term.GetState(slave.Fd())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Error("console did not restore terminal modes")
			}
			lock.Lock()
			got := output.String()
			lock.Unlock()
			if colored := strings.Contains(got, "38;") || strings.Contains(got, "48;"); colored != (exit != "no-color") {
				t.Errorf("unexpected terminal color for %s: %q", exit, got)
			}
		})
	}
}
