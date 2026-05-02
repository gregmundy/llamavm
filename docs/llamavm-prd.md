# llamavm — Product Requirements Document

**Version:** 1.0
**Status:** Approved for v1 implementation
**Author:** Greg Mundy
**Date:** 2026-05-02

---

## 1. Overview

### 1.1 Summary

llamavm is a version manager for [llama.cpp](https://github.com/ggml-org/llama.cpp) on Apple Silicon Macs. It builds llama.cpp from source into versioned, isolated directories; manages an active version via shell shims; supports per-project version pinning via a `.llama-version` file; and benchmarks installed versions against a model so users can detect performance regressions across releases.

### 1.2 Tagline

*Run any llama.cpp version. Switch instantly. Catch regressions before they bite.*

### 1.3 Problem statement

llama.cpp ships frequent releases — sometimes multiple per week. Releases occasionally introduce performance regressions, bugs that affect specific models, or changes in command-line behavior. Engineers running local LLMs need a way to:

1. Have multiple llama.cpp versions installed simultaneously
2. Switch between them without rebuilding or fighting PATH
3. Pin a specific version per-project so different projects can use different versions
4. Compare versions empirically when investigating performance issues or regressions

Today, the options are:

- **Homebrew** — installs one version at a time; no version pinning; no benchmarking
- **Build from source manually** — works, but each user reinvents the same scripts to manage multiple builds
- **asdf/mise plugins** — none exist for llama.cpp; writing one limits the audience to people who already use those tools
- **Container-based isolation** — heavyweight; introduces overhead

llamavm fills this gap with a focused, single-purpose CLI tool modeled after `nvm`, `pyenv`, and `rbenv`.

### 1.4 Non-goals

llamavm is deliberately not:

- A model manager (HuggingFace and `huggingface-cli` solve this)
- A llama.cpp wrapper or higher-level inference SDK
- A tool for the Python bindings (`llama-cpp-python` is a different ecosystem)
- A multi-platform installer in v1 (Apple Silicon only; Linux/Windows deferred)
- A pre-built binary distributor (v1 builds from source on the user's machine)

---

## 2. Audience and use cases

### 2.1 Primary audience

Engineers running llama.cpp directly on Apple Silicon Macs. This includes:

- Developers building local-AI applications that depend on llama.cpp
- Engineers experimenting with new releases as they ship
- Researchers comparing inference performance across versions
- Hobbyists running local models for personal use

The primary audience explicitly excludes Linux/Windows users in v1, users of `llama-cpp-python` only, and users of higher-level platforms (LM Studio, Ollama) that abstract over llama.cpp entirely.

### 2.2 Use cases

**UC1: First-time setup**
A user has never used llamavm. They install it via Homebrew, add the shims directory to PATH, run `llamavm install latest`, and have a working `llama-cli` on PATH within 15 minutes.

**UC2: Side-by-side version comparison**
A user wants to know if a recent llama.cpp release introduced a regression on their model. They `llamavm install` the previous version, then run `llamavm bench all --model <path>` to see throughput across versions side-by-side.

**UC3: Per-project pinning**
A user has two projects: one stable on b5046, one experimental on b5489. They `cd` into each project's directory and `llamavm pin <version>`. Subsequent `llama-cli` calls automatically use the pinned version.

**UC4: Rolling back a bad release**
A user upgraded to a new release and something broke. They run `llamavm use <previous-version>` and are immediately back on the old version. No rebuild required because the previous version is still installed.

**UC5: Cleaning up**
After months of use, a user has 8 versions installed and wants to free disk space. They run `llamavm list`, decide which versions to keep, and `llamavm uninstall <version>` for the rest.

---

## 3. Functional requirements

### 3.1 Command surface

The complete v1 command surface:

| Command | Purpose |
|---|---|
| `llamavm install <version>` | Build and install a specific version from source |
| `llamavm install latest` | Build and install the most recent stable release |
| `llamavm uninstall <version>` | Remove an installed version |
| `llamavm list` | Show installed versions; mark the active one |
| `llamavm list-remote` | Show available versions from GitHub releases |
| `llamavm use <version>` | Set the global active version |
| `llamavm current` | Print the currently active version |
| `llamavm pin <version>` | Write `.llama-version` in the current working directory |
| `llamavm bench <version>` | Benchmark a single installed version against a model |
| `llamavm bench all` | Benchmark every installed version and print comparison |
| `llamavm doctor` | Verify shims are installed and PATH is configured correctly |
| `llamavm --help` | Show top-level help |
| `llamavm <subcommand> --help` | Show subcommand help |
| `llamavm --version` | Print llamavm's own version |

No flags beyond standard `--help` and `--version` exist on subcommands in v1, except where documented below.

### 3.2 Version identifiers

llamavm uses llama.cpp's release tag format directly: `b5046`, `b5489`, etc. The `b` prefix is part of the tag and is required. There is no semver-like version format; this matches llama.cpp's actual versioning scheme.

The keyword `latest` resolves to the most recent release tag from GitHub at install time. Once installed, the version is referenced by its actual tag, not by `latest`.

### 3.3 `install` subcommand

#### 3.3.1 Behavior

`llamavm install <version>` performs the following sequence:

1. Validate the version exists by querying `https://api.github.com/repos/ggml-org/llama.cpp/releases/tags/<version>`
2. If the version is already installed, exit with a message and zero status (idempotent)
3. Create `~/.llamavm/versions/<version>/` and a `source/` subdirectory inside it
4. Run `git clone --depth 1 --branch <version> https://github.com/ggml-org/llama.cpp.git ~/.llamavm/versions/<version>/source`
5. Detect platform (Apple Silicon Darwin) and set Metal-optimized build flags
6. Run `cmake -B build -DGGML_METAL=ON -DCMAKE_BUILD_TYPE=Release` in the source directory
7. Run `cmake --build build --config Release -j $(sysctl -n hw.ncpu)`
8. Symlink `build/bin/llama-cli`, `build/bin/llama-server`, and `build/bin/llama-quantize` into `~/.llamavm/versions/<version>/bin/`
9. If shims do not yet exist in `~/.llamavm/shims/`, install them
10. If no version is currently active, set this version as active
11. Print a success message including the install path and elapsed time

#### 3.3.2 `install latest`

When invoked with `latest`, the command first queries `https://api.github.com/repos/ggml-org/llama.cpp/releases/latest` to resolve the actual tag name, then proceeds as `install <resolved-tag>`.

#### 3.3.3 Failure handling

If any step after directory creation fails:

1. The build output is written to `~/.llamavm/versions/<version>/build.log`
2. The version directory is removed
3. The CLI exits non-zero with an error message that points to the build log

This guarantees that `llamavm list` never shows a half-installed version.

#### 3.3.4 Network requirements

The `install` command requires network access for:

- GitHub API (release metadata)
- GitHub clone (source code)

If either fails, the command exits non-zero with a clear error message indicating which network operation failed.

#### 3.3.5 Build prerequisites

The user is expected to have:

- Xcode Command Line Tools (`xcode-select --install`)
- `cmake` (Homebrew or otherwise)
- `git`

If `cmake` is missing, `install` exits with an error message instructing the user to install it. `git` and Xcode CLT are checked the same way. llamavm does not install these dependencies itself.

### 3.4 `uninstall` subcommand

`llamavm uninstall <version>` removes `~/.llamavm/versions/<version>/` and its associated cached benchmarks in `~/.llamavm/benchmarks/`.

If the uninstalled version was the currently active version, the active version is cleared. The next shim invocation will fail with a clear error message instructing the user to set an active version. There is no "automatic fallback" to a different installed version; explicit user choice is preferred.

If the version is not installed, the command exits non-zero with a clear error.

### 3.5 `list` subcommand

`llamavm list` prints installed versions, one per line, with the active version marked with an asterisk:

```
  b5489
* b5046
  b5400
```

If no versions are installed, prints a single line: `No versions installed. Run 'llamavm install latest' to install one.`

### 3.6 `list-remote` subcommand

`llamavm list-remote` queries the GitHub releases API and prints available tags, most recent first, with a default limit of 30 tags:

```
b5489 (latest)
b5488
b5487
...
```

Optional flag: `--limit <n>` overrides the default count. Optional flag: `--all` returns every release tag (potentially hundreds).

### 3.7 `use` subcommand

`llamavm use <version>` writes the version string to `~/.llamavm/current`. This file is the source of truth for the active version when no `.llama-version` file is found in the current working directory's ancestry.

If the version is not installed, the command exits non-zero with an error suggesting `llamavm install <version>`.

### 3.8 `current` subcommand

`llamavm current` prints the version that would be used by a shim invoked from the current working directory. The resolution order is:

1. `.llama-version` in the current directory or any ancestor up to the user's home directory
2. `~/.llamavm/current`
3. None set — print "No active version" and exit non-zero

The output is the version string with no decoration, suitable for use in scripts: `llamavm current` should print `b5046`, not `Current version: b5046`.

### 3.9 `pin` subcommand

`llamavm pin <version>` writes the version string (with no trailing newline issues) to `.llama-version` in the current working directory. If the file already exists, it is overwritten.

If the version is not installed, the command warns but still writes the file. The user may pin a version they intend to install later.

### 3.10 `bench` subcommand

#### 3.10.1 Single-version benchmark

`llamavm bench <version> --model <path-to-gguf>` performs:

1. Verify the version is installed
2. Verify the model file exists at the given path
3. Compute a SHA-256 of the first 4KB of the model file (cheap fingerprint for cache key)
4. Check `~/.llamavm/benchmarks/<version>_<model-fingerprint>.json` — if exists and `--no-cache` is not specified, print cached result and exit
5. Run the version's `llama-cli` with a fixed benchmark prompt
6. Parse stderr for tokens/sec and total time
7. Write result JSON to cache
8. Print result

The fixed benchmark prompt is hardcoded in v1 and is not configurable:

```
Write a detailed 200-word summary of the French Revolution. Include key dates, figures, and outcomes.
```

The fixed argument set is:

```
llama-cli -m <model> -p <prompt> -n 256 --no-display-prompt -ngl 99
```

(`-ngl 99` offloads all layers to Metal GPU. `-n 256` caps generation at 256 tokens for consistent timing.)

#### 3.10.2 Comparison benchmark

`llamavm bench all --model <path>` runs the benchmark against every installed version in sequence and prints a comparison table:

```
Benchmarking 3 versions on qwen2.5-7b-instruct-q4_k_m.gguf...

Version   Tokens/sec   Total Time   Status
b5489        38.2 t/s      18.4s    current
b5046        41.7 t/s      16.9s    +9.2% vs current
b5400        39.1 t/s      18.0s    +2.4% vs current

Best: b5046 (41.7 t/s)
```

Percentage delta is calculated relative to the currently active version. The "Best" line identifies the highest tokens/sec across the comparison.

#### 3.10.3 Model path resolution

The `--model <path>` argument accepts either an absolute path or a path relative to the current working directory. v1 does not search known model directories automatically.

#### 3.10.4 Cache invalidation

Cached benchmarks are keyed by `<version>_<model-fingerprint>`. If the user wants to re-run a benchmark, they pass `--no-cache`. The cache directory can be cleared manually with `rm -rf ~/.llamavm/benchmarks/`.

### 3.11 `doctor` subcommand

`llamavm doctor` performs the following checks and prints a status line for each:

1. `~/.llamavm` directory exists
2. `~/.llamavm/shims` directory exists and contains shims for `llama-cli`, `llama-server`, `llama-quantize`
3. `~/.llamavm/shims` is on PATH
4. At least one version is installed
5. `~/.llamavm/current` is set to a valid installed version (or `.llama-version` is present in cwd ancestry)
6. `cmake` is on PATH
7. `git` is on PATH
8. Xcode CLT is installed (`xcode-select -p` succeeds)

Each check prints either `✓` (passed) or `✗ <remediation>` (failed). If any check fails, `doctor` exits non-zero. The output is designed to be paste-able into a GitHub issue when seeking help.

### 3.12 Shims

#### 3.12.1 Shim installation

The first `llamavm install` creates three executables in `~/.llamavm/shims/`:

- `llama-cli`
- `llama-server`
- `llama-quantize`

These are small Go binaries (built from a single shim source) that share identical logic but are distinguished by their argv[0]. The shim binary itself is roughly 5-10MB compiled.

Subsequent `llamavm install` invocations do not recreate shims if they already exist.

#### 3.12.2 Shim resolution logic

When a shim is invoked, it executes the following logic:

1. Determine the requested binary name from `os.Args[0]` (e.g., `llama-cli`)
2. Resolve the active version using the same logic as `llamavm current`:
   - Walk up from cwd looking for `.llama-version` (stopping at the user's home directory)
   - Fall back to `~/.llamavm/current`
3. If no version is set, print an error to stderr and exit with code 127
4. Construct the path: `~/.llamavm/versions/<version>/bin/<binary-name>`
5. If the path doesn't exist, print an error to stderr and exit with code 127
6. `syscall.Exec` the binary with the original argv (excluding argv[0]) and the current environment

#### 3.12.3 PATH integration

The user adds the shims directory to PATH manually, in their shell rc file:

```bash
export PATH="$HOME/.llamavm/shims:$PATH"
```

llamavm does not modify the user's shell configuration. `llamavm doctor` checks for the PATH presence and provides this exact line as remediation.

### 3.13 `.llama-version` file format

The file contains a single line: the version string (e.g., `b5046`). Trailing whitespace and newlines are tolerated when reading. The file is plain ASCII; no comments, no other content.

---

## 4. Non-functional requirements

### 4.1 Performance

- `llamavm install <version>` is dominated by the build step (5-15 minutes on M-series chips). llamavm itself adds <1 second of overhead.
- `llamavm list`, `current`, `use`, `pin`, `doctor` all complete in <100ms.
- Shim invocation overhead is <50ms; users should not perceive a difference vs. running the binary directly.
- `llamavm bench` is dominated by the inference run. llamavm's own overhead is <1 second.

### 4.2 Reliability

- Failed installs leave no trace in `~/.llamavm/versions/` (atomicity via temp directory + rename).
- `llamavm list` never shows a partial install.
- Shim invocations do not corrupt state or modify `~/.llamavm/current`.
- All filesystem writes use atomic patterns (temp file + rename) where data integrity matters.

### 4.3 Compatibility

- v1 supports Apple Silicon Macs running macOS 14 (Sonoma) or later.
- v1 builds against llama.cpp tags from b4000 onward. Older tags may fail to build with current Xcode/cmake; this is acceptable.
- llamavm requires Go 1.22+ to build from source; binary releases target macOS 14+.

### 4.4 Security

- llamavm makes outbound HTTPS requests to `api.github.com` and `github.com` only.
- llamavm does not require root or sudo; all operations occur within `~/.llamavm/`.
- Shim execs the target binary directly; no shell interpolation of arguments.

### 4.5 Disk usage

- Each installed version consumes 500MB-1.5GB depending on whether build artifacts are kept.
- v1 retains the full source tree and build directory per version. v2 may add `llamavm prune` to remove build artifacts while keeping binaries.
- Benchmark cache is negligible (<1MB total even with hundreds of entries).

---

## 5. Architecture

### 5.1 Language and toolchain

- **Language:** Go 1.22+
- **CLI framework:** `github.com/spf13/cobra` (standard for Go CLIs, predictable structure)
- **HTTP client:** Go standard library `net/http`
- **JSON:** Go standard library `encoding/json`
- **Filesystem:** Go standard library `os`, `path/filepath`
- **Process exec:** `os/exec` for build commands; `syscall.Exec` for shim execs
- **Concurrency:** Single-threaded for v1; build commands inherit cmake's `-j` parallelism

### 5.2 Filesystem layout

```
~/.llamavm/
├── versions/
│   ├── b5046/
│   │   ├── source/         # cloned repo
│   │   ├── build.log       # build output (only on failure)
│   │   └── bin/            # symlinks to source/build/bin/*
│   ├── b5489/
│   └── b5400/
├── shims/                  # added to user's PATH
│   ├── llama-cli
│   ├── llama-server
│   └── llama-quantize
├── current                 # text file: active version, e.g., "b5046"
└── benchmarks/
    └── b5046_<model-fingerprint>.json
```

### 5.3 Repository structure

```
llamavm/
├── cmd/
│   ├── llamavm/
│   │   └── main.go
│   └── llamavm-shim/
│       └── main.go
├── internal/
│   ├── cli/
│   │   ├── install.go
│   │   ├── uninstall.go
│   │   ├── list.go
│   │   ├── use.go
│   │   ├── current.go
│   │   ├── pin.go
│   │   ├── bench.go
│   │   ├── doctor.go
│   │   └── root.go
│   ├── builder/
│   │   ├── builder.go      # cmake invocation, error handling
│   │   └── platform.go     # Apple Silicon detection
│   ├── github/
│   │   └── releases.go     # GitHub API client (releases endpoint)
│   ├── version/
│   │   ├── resolver.go     # current version resolution logic
│   │   └── store.go        # versions directory CRUD
│   ├── shim/
│   │   └── shim.go         # shim resolution and exec logic (shared with cmd/llamavm-shim)
│   └── bench/
│       ├── runner.go       # llama-cli invocation and timing
│       ├── parser.go       # parse llama.cpp stderr output
│       └── cache.go        # benchmark result caching
├── go.mod
├── go.sum
├── README.md
├── LICENSE
├── docs/
│   ├── prd.md              # this document
│   └── screenshots/
├── .github/
│   └── workflows/
│       └── ci.yml
├── .goreleaser.yml         # for binary releases
└── homebrew/
    └── llamavm.rb          # Homebrew formula (lives in separate tap repo, copied here for reference)
```

### 5.4 Two binaries, shared logic

`cmd/llamavm` is the user-facing CLI. `cmd/llamavm-shim` is the shim binary installed three times into `~/.llamavm/shims/` (once for each binary name; this is achieved via build-time copies or runtime argv inspection, see 5.5).

Both binaries link against `internal/shim` for resolution logic. `cmd/llamavm-shim/main.go` is intentionally minimal:

```go
package main

import (
    "os"
    "github.com/gregmundy/llamavm/internal/shim"
)

func main() {
    if err := shim.Exec(os.Args); err != nil {
        // shim writes its own errors to stderr; this is just final cleanup
        os.Exit(127)
    }
}
```

### 5.5 Shim distribution

A single shim binary is built and copied (or symlinked) three times into `~/.llamavm/shims/` with names `llama-cli`, `llama-server`, `llama-quantize`. The shim reads `os.Args[0]`, takes the basename, and uses that as the target binary name.

This means adding a new shim (e.g., `llama-tokenize`) in v2 requires only adding the name to a list; no per-shim binary needed.

### 5.6 GitHub API usage

llamavm queries the unauthenticated GitHub REST API. Rate limits are 60 requests per hour per IP, which is more than sufficient for normal usage. If a rate limit is hit, the error message instructs the user to wait or set `GITHUB_TOKEN` (which llamavm respects if present in env).

### 5.7 Concurrency

v1 is single-threaded for simplicity. The cmake build itself is parallelized via `-j $(sysctl -n hw.ncpu)`. Future versions may parallelize bench-all across versions, but v1 runs them sequentially to avoid contention for the GPU.

---

## 6. User experience

### 6.1 First-run experience

A new user installs llamavm via Homebrew:

```bash
brew install gregmundy/tap/llamavm
```

After install, llamavm prints a one-time setup message instructing the user to add the shims directory to PATH:

```
llamavm installed successfully.

Add the following line to your shell configuration (e.g., ~/.zshrc):

    export PATH="$HOME/.llamavm/shims:$PATH"

Then reload your shell, and run:

    llamavm install latest

Run 'llamavm doctor' to verify your setup at any time.
```

### 6.2 Output style

llamavm output adheres to the following principles:

- **Quiet by default.** Successful operations print one line of confirmation; failures print errors to stderr.
- **No emoji or color escapes** unless stdout is a TTY. Output is pipeable.
- **Progress indication during long operations.** During `install`, llamavm prints a status line every 30 seconds with elapsed time and current build phase.
- **Errors include remediation.** Every error message ends with a suggested next step.

### 6.3 Help text

`llamavm --help` and `llamavm <subcommand> --help` follow Cobra's default style. Examples are included in subcommand help.

### 6.4 Exit codes

- `0` — success
- `1` — generic error (failed network call, build failure, invalid arguments)
- `2` — user error (version not installed, no active version set)
- `127` — shim could not resolve binary (shell convention)

---

## 7. Distribution and installation

### 7.1 Homebrew

The primary distribution channel for v1 is a Homebrew tap maintained by the author:

```bash
brew install gregmundy/tap/llamavm
```

The tap repository is a separate `homebrew-tap` repo containing `Formula/llamavm.rb`. The formula references release tarballs published via GitHub Releases.

### 7.2 Go install

Users with Go installed can install directly:

```bash
go install github.com/gregmundy/llamavm/cmd/llamavm@latest
```

This installs `llamavm` to `$GOBIN`. The user is responsible for running `llamavm install` afterward to set up shims.

### 7.3 Binary releases

GoReleaser produces signed binary tarballs for each release tag, attached to the GitHub release. v1 targets:

- `darwin-arm64`

v2 may add `darwin-amd64`, `linux-arm64`, `linux-amd64`.

### 7.4 Versioning

llamavm itself uses semver: `v1.0.0`, `v1.1.0`, etc. This is unrelated to llama.cpp's tag-based versioning. The CLI prints its own version via `llamavm --version`.

---

## 8. Testing strategy

### 8.1 Unit tests

Each package in `internal/` has a corresponding `_test.go` file. Unit tests cover:

- Version resolution logic (the cwd-walk for `.llama-version`)
- GitHub API response parsing
- Benchmark output parsing
- Filesystem operations using `t.TempDir()` for isolation

Target coverage: 80%+ for `internal/`.

### 8.2 Integration tests

A small integration test suite verifies end-to-end flows against a fixture llama.cpp release. These tests are tagged `//go:build integration` and run only in CI. They require network access.

### 8.3 Manual testing

Each acceptance criterion from §9 is verified manually before a release tag is cut. The verification log is kept in `docs/release-checks/<version>.md`.

### 8.4 CI

GitHub Actions runs on every PR and push to `main`:

1. `go fmt -l . | tee /dev/stderr | grep .` (fail if anything needs formatting)
2. `go vet ./...`
3. `staticcheck ./...`
4. `go test ./...` (unit tests only; integration tests run on a separate workflow gated on `workflow_dispatch`)
5. `go build ./...`

---

## 9. Acceptance criteria for v1

llamavm v1.0.0 is releasable when **all** of the following hold:

1. **Clean install works:** `brew install gregmundy/tap/llamavm` succeeds on a Mac with no prior llamavm state.
2. **Setup message is clear:** First-run output instructs the user to set PATH; copy-pasteable line is provided.
3. **`llamavm install b5046` succeeds** on a clean Apple Silicon Mac with Xcode CLT and cmake installed.
4. **`llamavm install latest`** resolves the most recent release and installs it.
5. **`llamavm list`** shows installed versions with the active one marked.
6. **`llama-cli --version`** (via shim) reports the active version's actual version string.
7. **`.llama-version` resolution works:** Creating `.llama-version` in a directory with content `b5046` causes shims invoked from that directory to use b5046, regardless of `~/.llamavm/current`.
8. **`llamavm bench all --model <path>`** produces a comparison table for ≥2 installed versions.
9. **`llamavm uninstall b5046`** removes the version cleanly. If b5046 was active, the active version is cleared.
10. **`llamavm doctor`** returns nonzero exit code when shims aren't on PATH; returns zero when everything is wired correctly.
11. **Failed installs are atomic:** Killing the build mid-install leaves no entry in `llamavm list`.
12. **Quality gates pass:** `go fmt`, `go vet`, `staticcheck`, `go test` all clean.
13. **README is complete:** Tagline, install instructions, basic usage, screenshot of `bench` output, "why this exists" paragraph.
14. **Homebrew tap is published** at `github.com/gregmundy/homebrew-tap` with a working formula.
15. **GitHub release v1.0.0 is published** with darwin-arm64 binary attached.

---

## 10. Out of scope for v1

The following are explicitly deferred:

| Feature | Target |
|---|---|
| Linux support | v2 |
| Windows support | v3 or never |
| Pre-built llama.cpp binaries (skip the build step) | v2 |
| Custom build flag profiles (`--cuda`, `--vulkan`, `--no-metal`) | v2 |
| Configurable benchmark prompts | v2 |
| Multiple-run benchmarks with confidence intervals | v2 |
| Auto-update notifications | v3 |
| `llamavm prune` to remove build artifacts | v2 |
| Automatic model path discovery (LM Studio, Ollama dirs) | v2 |
| Plugin system | Never |
| Web UI / TUI | Never |
| Model management | Never (use `huggingface-cli`) |
| Python bindings support | Never (different tool) |
| Daemon / server mode | Never |
| Telemetry / analytics | Never |

The "Never" entries are firm. If a feature appears that maps to a "Never" row, it is not added; instead, the relevant ecosystem tool is recommended.

---

## 11. Implementation plan

### 11.1 Milestones

The implementation is decomposed into five PRs, each independently shippable and reviewable:

**M1: Scaffolding + install + uninstall + list**

The hardest part. Build pipeline, error handling, atomicity, version directory layout. Once this works, the rest is mostly orchestration.

Acceptance: install b5046, list shows it, uninstall removes it. No shims yet; binaries are accessed via full path.

**M2: Shims + use + current**

The version-switching mechanic. Shim binary, shim installation during install, `~/.llamavm/current` file, `use` and `current` subcommands.

Acceptance: `llama-cli --version` (via shim) reports the active version's string; switching with `use` is reflected immediately.

**M3: `.llama-version` resolution**

The cwd-walk logic in the shim and `current` subcommand. `pin` subcommand.

Acceptance: pinning a version in a directory causes shims invoked from that directory to use the pinned version.

**M4: bench (single + comparison)**

Benchmark runner, llama.cpp output parser, cache. The `bench` and `bench all` commands.

Acceptance: `bench all --model <path>` produces a comparison table for ≥2 versions.

**M5: doctor + Homebrew tap + README + release**

Polish, distribution, documentation. The `doctor` subcommand. Homebrew tap setup. README with screenshots. v1.0.0 release.

Acceptance: `brew install gregmundy/tap/llamavm` works on a fresh Mac; README has all required sections; v1.0.0 release is published.

### 11.2 Estimated effort

- M1: 2-3 evenings
- M2: 1-2 evenings
- M3: 1 evening
- M4: 2 evenings
- M5: 2 evenings

Total: roughly 8-10 evenings of focused work, or 3-4 weekends.

### 11.3 Sequencing

The milestones are strictly sequential. Each PR merges to `main` before the next begins. No parallel work in v1.

---

## 12. Risks and open questions

### 12.1 Risks

**R1: llama.cpp build breaks on a future Xcode/cmake combination.** Mitigation: pin the build to known-good toolchains in CI; document the requirement clearly. v1 acceptance assumes current stable Xcode CLT.

**R2: GitHub API rate limits hit during heavy `list-remote` use.** Mitigation: respect `GITHUB_TOKEN` env var; cache release listings for 5 minutes.

**R3: Benchmark results are noisy and lead to false-positive regression reports.** Mitigation: v1 documents that single-run benchmarks are indicative, not authoritative. v2 adds multi-run with confidence intervals.

**R4: Disk usage spirals as users install many versions.** Mitigation: v1 documents disk usage per version. v2 adds `llamavm prune`.

**R5: Apple Silicon-only stance limits audience.** Accepted; this is the v1 scope.

### 12.2 Open questions

None blocking v1. The following are explicitly deferred:

- Whether to provide pre-built llama.cpp binaries (would require infrastructure for hosting, signing, version matrix). Punted to v2.
- Whether to integrate with HuggingFace for model discovery in `bench`. Punted to v2.
- Whether to support custom shell integrations (zsh completion, fish, etc.). Considered but deferred; v1 keeps shell integration to a single PATH line.

---

## 13. Success metrics

llamavm v1 is successful if:

- The author uses it daily within one week of v1.0.0 release
- At least one person besides the author installs it via Homebrew within 30 days of release
- A GitHub Issue or discussion thread surfaces real-world usage feedback within 60 days
- v1.1.0 ships within 90 days, addressing the highest-frequency feedback

The author commits to writing a "lessons learned" blog post at the v1.1.0 milestone, which serves as both retrospective and content for audience-building.

---

## Appendix A: Sample CLI sessions

### Installing and using

```
$ brew install gregmundy/tap/llamavm
$ export PATH="$HOME/.llamavm/shims:$PATH"  # add to ~/.zshrc
$ llamavm install latest
Installing b5489...
Cloning llama.cpp at b5489...
Building (this takes 5-15 minutes)...
✓ Installed b5489 in 8m 24s
✓ Set b5489 as active version

$ llama-cli --version
version: 5489 (commit deadbeef)

$ llamavm install b5046
Installing b5046...
✓ Installed b5046 in 9m 12s
b5489 remains active

$ llamavm list
* b5489
  b5046

$ llamavm use b5046
Active version: b5046

$ llama-cli --version
version: 5046 (commit cafebabe)
```

### Per-project pinning

```
$ cd ~/projects/jabbar
$ llamavm pin b5046
Pinned b5046 in ~/projects/jabbar/.llama-version

$ cd ~/projects/experimental
$ llamavm pin b5489

$ cd ~/projects/jabbar
$ llama-cli --version
version: 5046

$ cd ~/projects/experimental
$ llama-cli --version
version: 5489
```

### Benchmarking

```
$ llamavm bench all --model ~/Models/qwen2.5-7b-instruct-q4_k_m.gguf
Benchmarking 3 versions on qwen2.5-7b-instruct-q4_k_m.gguf...

Version   Tokens/sec   Total Time   Status
b5489        38.2 t/s      18.4s    current
b5046        41.7 t/s      16.9s    +9.2% vs current
b5400        39.1 t/s      18.0s    +2.4% vs current

Best: b5046 (41.7 t/s)
```

### Doctor

```
$ llamavm doctor
✓ ~/.llamavm exists
✓ Shims installed
✓ Shims directory on PATH
✓ At least one version installed (3)
✓ Active version set: b5046
✓ cmake on PATH
✓ git on PATH
✓ Xcode CLT installed

All checks passed.
```

---

## Appendix B: Glossary

- **Shim** — A small executable that intercepts calls to a versioned binary and routes them to the active version. Pattern used by `pyenv`, `rbenv`, `nodenv`.
- **Active version** — The version that shims will resolve to. Determined by `.llama-version` (if present in cwd ancestry) or `~/.llamavm/current`.
- **Tag** — A llama.cpp release tag, e.g., `b5046`. Used as the version identifier throughout llamavm.
- **GGUF** — The model file format used by llama.cpp.
- **Tokens/sec** — Inference throughput metric, parsed from llama.cpp's stderr output.
