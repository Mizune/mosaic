// Command mosaic is the entry point for the mosaic CLI / TUI.
package main

import (
	"fmt"
	"os"
)

// version is overridden at build time via -ldflags '-X main.version=...'.
var version = "dev"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
