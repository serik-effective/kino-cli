package main

import (
	"fmt"
	"os"

	"github.com/serik-effective/kino-cli/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
