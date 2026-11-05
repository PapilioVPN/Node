package xray

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/papilio-platform/Node/internal/instance"
)

// runtimeFiles owns one private directory; ownership transfers to Session only
// after the child starts. Closing it never removes the caller-owned parent.
type runtimeFiles struct {
	directory string
}

func (f runtimeFiles) close() error {
	return os.RemoveAll(f.directory)
}

type preparedConfiguration struct {
	files  runtimeFiles
	socket string
	input  nativeInput
}

func prepareRuntimeConfiguration(ctx context.Context, source []byte, runtimeDir string, checker *Checker) (prepared preparedConfiguration, err error) {
	directory, err := os.MkdirTemp(runtimeDir, "xray-")
	if err != nil {
		return prepared, fmt.Errorf("create Xray runtime directory: %w", err)
	}
	prepared.files.directory = directory
	defer func() {
		if err != nil {
			if cleanupErr := prepared.files.close(); cleanupErr != nil {
				err = errors.Join(err, instance.ErrCleanupIncomplete, cleanupErr)
			}
		}
	}()
	prepared.socket = filepath.Join(directory, "api.sock")
	if len(prepared.socket) > 107 {
		return prepared, errors.New("Xray private socket path exceeds the Linux path limit")
	}
	overlay, err := prepareAPIOverlay(source, prepared.socket)
	if err != nil {
		return prepared, fmt.Errorf("prepare Xray API configuration: %w", err)
	}
	path := filepath.Join(directory, "api.json")
	if err := os.WriteFile(path, overlay, 0600); err != nil {
		return prepared, fmt.Errorf("write Xray API configuration: %w", err)
	}
	prepared.input = nativeInput{format: "json", paths: []string{"stdin:", path}, stdin: source}
	selection, err := selectEmpty2022(source)
	if err != nil {
		return prepared, fmt.Errorf("inspect Xray managed membership: %w", err)
	}
	if len(selection.empties) != 0 {
		compiled := filepath.Join(directory, "effective.pb")
		if err := checker.compile(ctx, source, path, compiled, selection); err != nil {
			return prepared, err
		}
		prepared.input = nativeInput{format: "protobuf", paths: []string{compiled}}
	}
	return prepared, nil
}
