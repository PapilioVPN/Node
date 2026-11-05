package xray

import (
	"context"
	"crypto/rand"
	"net/url"

	proxyman "github.com/xtls/xray-core/app/proxyman/command"
	"github.com/xtls/xray-core/app/stats/command"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type nativeAPI struct {
	connection  *grpc.ClientConn
	stats       command.StatsServiceClient
	handlers    proxyman.HandlerServiceClient
	revisionKey [32]byte
}

func newNativeAPI(socket string) (*nativeAPI, error) {
	var revisionKey [32]byte
	if _, err := rand.Read(revisionKey[:]); err != nil {
		return nil, err
	}
	target := (&url.URL{Scheme: "unix", Path: socket}).String()
	connection, err := grpc.NewClient(target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDisableRetry(),
	)
	if err != nil {
		return nil, err
	}
	return &nativeAPI{
		connection:  connection,
		stats:       command.NewStatsServiceClient(connection),
		handlers:    proxyman.NewHandlerServiceClient(connection),
		revisionKey: revisionKey,
	}, nil
}

func (a *nativeAPI) probe(ctx context.Context) error {
	_, err := a.stats.GetSysStats(ctx, &command.SysStatsRequest{}, grpc.WaitForReady(true))
	return err
}
