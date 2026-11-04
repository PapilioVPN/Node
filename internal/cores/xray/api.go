package xray

import (
	"context"
	"net/url"

	"github.com/xtls/xray-core/app/stats/command"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type nativeAPI struct {
	connection *grpc.ClientConn
	stats      command.StatsServiceClient
}

func newNativeAPI(socket string) (*nativeAPI, error) {
	target := (&url.URL{Scheme: "unix", Path: socket}).String()
	connection, err := grpc.NewClient(target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDisableRetry(),
	)
	if err != nil {
		return nil, err
	}
	return &nativeAPI{connection: connection, stats: command.NewStatsServiceClient(connection)}, nil
}

func (a *nativeAPI) probe(ctx context.Context) error {
	_, err := a.stats.GetSysStats(ctx, &command.SysStatsRequest{}, grpc.WaitForReady(true))
	return err
}
