package xray

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
)

const maxCompiledConfigBytes = 64 << 20

func (c *Checker) compile(ctx context.Context, source []byte, overlay, destination string, selection managed2022Selection) error {
	ctx, cancel := context.WithTimeout(ctx, c.options.Timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	// Pre-create the native output with private permissions; the child truncates it.
	if err := os.WriteFile(destination, nil, 0600); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, c.options.Executable, "convert", "pb", "-outpbfile", destination, "stdin:", overlay)
	cmd.Dir, cmd.Env = c.options.WorkingDir, nativeEnvironment(c.options.AssetsDir)
	cmd.Stdin = bytes.NewReader(source)
	output := diagnosticTail{data: make([]byte, c.options.MaxDiagnosticBytes)}
	cmd.Stdout, cmd.Stderr, cmd.WaitDelay = &output, &output, c.options.WaitDelay
	err := cmd.Run()
	if ctx.Err() != nil {
		err = errors.Join(ctx.Err(), err)
	}
	if err != nil {
		return &CheckError{cause: err, Diagnostics: output.bytes(), Truncated: output.truncated}
	}
	file, err := os.Open(destination)
	if err != nil {
		return err
	}
	data, readErr := readCompiledBytes(file, maxCompiledConfigBytes)
	if err := errors.Join(readErr, file.Close()); err != nil {
		return err
	}
	data, err = rewriteEmpty2022(data, selection)
	if err != nil {
		return fmt.Errorf("prepare empty Xray account membership: %w", err)
	}
	if len(data) > maxCompiledConfigBytes {
		return ErrConfigTooLarge
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.WriteFile(destination, data, 0600)
}

func readCompiledBytes(reader io.Reader, limit int) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, ErrConfigTooLarge
	}
	return data, nil
}
