# llamavm

> A version manager for [llama.cpp](https://github.com/ggml-org/llama.cpp) on Apple Silicon. Like `nvm` or `pyenv`, but for `llama-cli`.

## Why this exists

llama.cpp ships fast — multiple releases per week — and individual builds occasionally regress performance or change behavior. There is no asdf/mise plugin and Homebrew installs only one version at a time. llamavm builds versioned releases from source into `~/.llamavm/versions/<tag>/`, switches the active version via shims on PATH, supports per-project pinning via `.llama-version`, and benchmarks installed versions against a model so you can quickly tell which build is fastest on your hardware.

## Requirements

- Apple Silicon Mac running macOS 14 (Sonoma) or later
- Xcode Command Line Tools (`xcode-select --install`)
- `cmake` (`brew install cmake`)

Run `llamavm doctor` at any time to verify your environment.

## Install

```bash
brew install gregmundy/tap/llamavm
```

Then add the shims directory to your PATH (one-time, in your shell rc):

```bash
export PATH="$HOME/.llamavm/shims:$PATH"
```

## Quickstart

```bash
# Install the latest llama.cpp release
llamavm install latest

# List installed versions; the active one is marked with *
llamavm list

# Switch the global active version
llamavm use b5046

# Pin a specific version for the current project
llamavm pin b5046

# Confirm everything is wired correctly
llamavm doctor
```

`llama-cli`, `llama-server`, and `llama-quantize` are now on your PATH and dispatch to the active version automatically. Per-directory pinning takes precedence over the global `current` file.

## Benchmarking

Compare every installed version against a model:

```
$ llamavm bench all --model ~/models/llama-3.2-1b-instruct-q4_k_m.gguf
Version  Tokens/s   Total time   Δ vs current
b5046    44.72       9.8s         baseline (current)
b5489    47.21       9.2s         +5.6%
b5400    43.10      10.1s         -3.6%

Best: b5489 (47.21 tok/s)
```

Single-version run:

```bash
llamavm bench b5046 --model ~/models/llama-3.2-1b-instruct-q4_k_m.gguf
```

Results are cached by `(version, model-fingerprint)` under `~/.llamavm/benchmarks/`. Pass `--no-cache` to force a re-run.

## Commands

| Command | What it does |
| --- | --- |
| `llamavm install <tag>` | Build the given llama.cpp release tag and install it |
| `llamavm install latest` | Resolve the most recent release and install it |
| `llamavm uninstall <tag>` | Remove a previously installed version |
| `llamavm list` | Show installed versions; active one marked with `*` |
| `llamavm list-remote` | Show the most recent llama.cpp releases on GitHub |
| `llamavm use <tag>` | Set the global active version |
| `llamavm current` | Print the currently active version (respects `.llama-version`) |
| `llamavm pin <tag>` | Write `.llama-version` in the current directory |
| `llamavm bench <tag> --model <path>` | Benchmark a single version |
| `llamavm bench all --model <path>` | Benchmark every installed version |
| `llamavm doctor` | Diagnose installation and PATH configuration |

Run any subcommand with `--help` for full options.

## How it works

`llamavm install <tag>` clones llama.cpp at the given tag into a staging directory, runs the standard cmake build with Metal enabled, and atomically renames the result into `~/.llamavm/versions/<tag>/`. Failed builds leave no trace in `llamavm list`.

The first install also drops three small Go binaries — `llama-cli`, `llama-server`, `llama-quantize` — into `~/.llamavm/shims/`. When invoked, each shim walks up from the current directory looking for `.llama-version`, then falls back to `~/.llamavm/current`, then `exec`s the corresponding binary inside the resolved version's directory. Shim overhead is under 50ms.

## Uninstall

```bash
brew uninstall llamavm
rm -rf ~/.llamavm
```

## License

MIT
