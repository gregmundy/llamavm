package shim

import "errors"

// Exec resolves the active llama.cpp version and execs the requested binary.
// Real implementation lands in M2 (shims + use + current).
func Exec(args []string) error {
	return errors.New("shim: not yet implemented")
}
