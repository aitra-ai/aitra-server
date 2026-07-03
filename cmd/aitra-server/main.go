package main

import (
	"context"
	"os"

	"github.com/aitra-ai/aitra-server/cmd/aitra-server/cmd"
)

func main() {
	command := cmd.RootCmd
	if err := command.ExecuteContext(context.Background()); err != nil {
		os.Exit(1)
	}
}
