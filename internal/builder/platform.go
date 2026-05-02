package builder

import "runtime"

// Platform exposes host-environment facts the build flow cares about.
type Platform interface {
	IsAppleSilicon() bool
	Cores() int
}

type defaultPlatform struct{}

func (defaultPlatform) IsAppleSilicon() bool {
	return runtime.GOOS == "darwin" && runtime.GOARCH == "arm64"
}

func (defaultPlatform) Cores() int { return runtime.NumCPU() }

// DefaultPlatform is the production Platform implementation.
var DefaultPlatform Platform = defaultPlatform{}
