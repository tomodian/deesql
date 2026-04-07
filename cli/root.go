package cli

import "github.com/urfave/cli/v3"

func App() *cli.Command {
	return &cli.Command{
		Name:  "dsql-migrate",
		Usage: "Schema migration tool for Amazon Aurora DSQL",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  FlagEndpoint,
				Usage: "Aurora DSQL cluster endpoint",
			},
			&cli.StringFlag{
				Name:  FlagRegion,
				Usage: "AWS region (auto-detected from endpoint if omitted)",
			},
			&cli.StringFlag{
				Name:  FlagUser,
				Usage: "Database user",
				Value: DefaultUser,
			},
			&cli.StringSliceFlag{
				Name:  FlagSchema,
				Usage: "SQL files or directories with desired-state .sql files",
			},
			&cli.StringFlag{
				Name:    FlagProfile,
				Usage:   "AWS profile name (from ~/.aws/config)",
				Sources: cli.EnvVars("AWS_PROFILE"),
			},
			&cli.StringFlag{
				Name:  FlagRoleARN,
				Usage: "IAM role ARN to assume",
			},
			&cli.DurationFlag{
				Name:  FlagConnectTimeout,
				Usage: "Database connection timeout",
				Value: DefaultConnectTimeout,
			},
		},
		Commands: []*cli.Command{
			planCmd(),
			applyCmd(),
			verifyCmd(),
		},
	}
}
