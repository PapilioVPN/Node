package xray

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

func readVersion(ctx context.Context, options CheckerOptions) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, options.Executable, "version")
	cmd.Dir, cmd.Env = options.WorkingDir, nativeEnvironment(options.AssetsDir)
	cmd.WaitDelay = options.WaitDelay
	output := &diagnosticTail{data: make([]byte, options.MaxDiagnosticBytes)}
	cmd.Stdout, cmd.Stderr = output, output
	err := cmd.Run()
	if ctx.Err() != nil {
		err = errors.Join(ctx.Err(), err)
	}
	if err != nil {
		return "", fmt.Errorf("read Xray version: %w", err)
	}
	if output.truncated {
		return "", errors.New("Xray version output exceeds the diagnostic byte limit")
	}
	line, _, _ := strings.Cut(string(output.bytes()), "\n")
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "Xray" {
		return "", errors.New("unrecognized Xray version response")
	}
	return fields[1], nil
}
