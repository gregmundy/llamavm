package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	root := &cobra.Command{
		Use:          "llamavm",
		Short:        "A version manager for llama.cpp on Apple Silicon",
		Version:      version,
		SilenceUsage: true,
	}

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
