package main

import (
	"os"
)

func main() {
	rootCmd := newRootCmd()
	if err := rootCmd.Execute(); err != nil {
		// Exit code 2 for usage errors, 1 for fatal runtime errors
		os.Exit(ExitFatal)
	}
}
