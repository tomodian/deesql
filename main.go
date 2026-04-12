package main

import (
	"context"
	"fmt"
	"os"

	"github.com/tomodian/deesql/cli"
)

func main() {
	if err := cli.App().Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
