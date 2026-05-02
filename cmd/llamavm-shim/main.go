package main

import (
	"os"

	"github.com/gregmundy/llamavm/internal/shim"
)

func main() {
	if err := shim.Exec(os.Args); err != nil {
		os.Exit(127)
	}
}
