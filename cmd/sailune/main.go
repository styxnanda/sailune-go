package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/styxnanda/sailune-go/internal/cli"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := cli.RunContext(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "sailune:", err)
		os.Exit(1)
	}
}
