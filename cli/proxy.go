package cli

import (
	"context"

	"github.com/urfave/cli/v3"

	"tomodian/deesql/internal/proxy"
)

func proxyCmd() *cli.Command {
	return &cli.Command{
		Name:  "proxy",
		Usage: "Start a DSQL-filtering proxy between client and PostgreSQL",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  FlagListen,
				Usage: "Address to listen on (e.g. :15432)",
				Value: DefaultListen,
			},
			&cli.StringFlag{
				Name:  FlagUpstream,
				Usage: "Backend PostgreSQL address (e.g. localhost:5432)",
				Value: DefaultUpstream,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return proxy.Run(ctx, proxy.RunInput{
				ListenAddr:   cmd.String(FlagListen),
				UpstreamAddr: cmd.String(FlagUpstream),
			})
		},
	}
}
