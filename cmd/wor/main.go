package main

import (
	"context"
	"os"

	"github.com/team-worapong/wor/internal/cli"
)

func main() {
	os.Exit(cli.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
