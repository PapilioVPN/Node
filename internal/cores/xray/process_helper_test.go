package xray_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/xtls/xray-core/app/stats/command"
	"google.golang.org/grpc"
)

type helperRequest struct {
	Mode       string
	ReadyFile  string
	WorkingDir string
	AssetsDir  string
}

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "version" {
		switch filepath.Base(os.Args[0]) {
		case "version-alternate":
			fmt.Fprintln(os.Stdout, "Xray 25.3.6")
			os.Exit(0)
		case "version-malformed":
			fmt.Fprintln(os.Stdout, "another engine")
			os.Exit(0)
		case "version-overflow":
			fmt.Fprint(os.Stdout, strings.Repeat("x", 80*1024))
			os.Exit(0)
		case "version-exit":
			fmt.Fprint(os.Stderr, "private-version-diagnostic")
			os.Exit(17)
		case "version-block":
			for {
				time.Sleep(time.Hour)
			}
		}
		fmt.Fprintln(os.Stdout, "Xray 26.10.31")
		os.Exit(0)
	}
	if len(os.Args) > 1 && os.Args[1] == "run" {
		os.Exit(runXrayHelper())
	}
	os.Exit(m.Run())
}

// A separate process injects deterministic OS failures; real engine behavior is
// covered by tests/integration against the checksum-pinned Xray executable.
func runXrayHelper() int {
	args := os.Args[1:]
	var overlay string
	checking := len(args) > 1 && args[1] == "-test"
	wantArgs := []string{"run", "-format=json", "-c", "stdin:"}
	if checking {
		wantArgs = []string{"run", "-test", "-format=json", "-c", "stdin:"}
	}
	if len(args) == len(wantArgs)+2 && args[len(args)-2] == "-c" && filepath.IsAbs(args[len(args)-1]) {
		overlay = args[len(args)-1]
		if _, err := os.Stat(args[len(args)-1]); err != nil {
			return 96
		}
		args = args[:len(args)-2]
	}
	if !slices.Equal(args, wantArgs) {
		return 90
	}
	var request helperRequest
	if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
		return 91
	}
	switch request.Mode {
	case "environment":
		cwd, err := os.Getwd()
		if err != nil || cwd != request.WorkingDir {
			return 92
		}
		want := []string{}
		if request.AssetsDir != "" {
			want = append(want, "XRAY_LOCATION_ASSET="+request.AssetsDir)
		}
		if !slices.Equal(os.Environ(), want) {
			return 93
		}
	case "output":
		fmt.Fprint(os.Stdout, strings.Repeat("x", 80*1024))
		fmt.Fprint(os.Stderr, "private-tail")
		return 23
	case "block":
		if err := os.WriteFile(request.ReadyFile, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
			return 94
		}
		for {
			time.Sleep(time.Hour)
		}
	case "block-after-validation":
		if checking {
			return 0
		}
		signal.Ignore(syscall.SIGTERM)
		if err := os.WriteFile(request.ReadyFile, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
			return 94
		}
		for {
			time.Sleep(time.Hour)
		}
	case "exit-after-validation":
		if !checking {
			fmt.Fprint(os.Stderr, strings.Repeat("x", 80*1024)+"private-exit-tail")
			return 42
		}
	case "api-with-default-signal":
		if checking {
			return 0
		}
		var configuration struct {
			API struct{ Listen string }
		}
		data, err := os.ReadFile(overlay)
		if err != nil || json.Unmarshal(data, &configuration) != nil {
			return 97
		}
		listener, err := net.Listen("unix", configuration.API.Listen)
		if err != nil {
			return 98
		}
		server := grpc.NewServer()
		command.RegisterStatsServiceServer(server, helperStatsService{})
		if err := server.Serve(listener); err != nil {
			return 99
		}
	case "success":
	default:
		return 95
	}
	return 0
}

type helperStatsService struct {
	command.UnimplementedStatsServiceServer
}

func (helperStatsService) GetSysStats(context.Context, *command.SysStatsRequest) (*command.SysStatsResponse, error) {
	return &command.SysStatsResponse{NumGoroutine: 1}, nil
}

func helperConfig(t *testing.T, request helperRequest) []byte {
	t.Helper()
	config, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	return config
}
