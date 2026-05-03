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

# Confirm everything is wired correctly (8/8 ✓ when ready)
llamavm doctor

# List installed versions; the active one is marked with *
llamavm list

# Install another version and compare side-by-side
llamavm install b9009
llamavm use latest          # switches active to the newest installed tag

# Pin a specific version for the current project
cd ~/myproject
llamavm pin b9009           # writes .llama-version here
```

`llama-cli`, `llama-server`, and `llama-quantize` are now on your PATH and dispatch to the active version automatically. Per-directory pinning takes precedence over the global `current` file.

## Diagnostics

`llamavm info` is the one-screen view of what's running and why:

```
$ llamavm info
Tag:    b9010
Source: from /Users/greg/.llamavm/current
Build:  d05fe1d (llama.cpp git SHA)
Path:   /Users/greg/.llamavm/versions/b9010
Built:  2026-05-02 20:50 EDT
```

In a directory with a pin file, `Source:` will show `pinned at <path>` instead, so you always know where the resolved tag came from. The `Build:` SHA is what `llama-cli --version` reports — handy for cross-referencing bug reports against upstream commits.

For a quick scriptable check:

```bash
llamavm current             # → b9010 (just the tag)
llamavm current -v          # → b9010 (from /Users/greg/.llamavm/current)
```

## Benchmarking

Compare every installed version against a model:

```
$ llamavm bench all --model ~/models/gemma-4-E4B-it-Q4_K_M.gguf
Version   Tokens/sec   Total Time   Status
b9009     38.0 t/s     5.0s         +5.6% vs current
b9010     36.0 t/s     5.1s         current
b8500     35.9 t/s     5.2s         ≈ current

Best: b9009 (38.0 t/s)
```

Differences below 0.5% are reported as `≈ current` rather than a small signed delta — single-run benchmarks can't distinguish that signal from run-to-run jitter. If the best version is also within noise of the current one, the `Best:` line reads `current (within noise)` instead of crowning a non-winner.

Single-version run:

```bash
llamavm bench b9010 --model ~/models/gemma-4-E4B-it-Q4_K_M.gguf
```

Internally this drives `llama-bench` with `-p 256 -n 128 -ngl 99 -r 1` against your model and records the token-generation throughput (`tg<N>` row). Results are cached by `(version, model-fingerprint)` under `~/.llamavm/benchmarks/`; pass `--no-cache` to force a re-run.

## Commands

| Command | What it does |
| --- | --- |
| `llamavm install <tag>` | Build the given llama.cpp release tag and install it |
| `llamavm install latest` | Resolve the most recent release and install it |
| `llamavm uninstall <tag>` | Remove a previously installed version |
| `llamavm list` | Show installed versions; active one marked with `*` |
| `llamavm list-remote` | Show the most recent llama.cpp releases on GitHub |
| `llamavm use <tag>` | Set the global active version (`<tag>` may be `latest`) |
| `llamavm current [-v]` | Print the active version (respects `.llama-version`); `-v` shows resolution source |
| `llamavm pin <tag>` | Write `.llama-version` in the current directory |
| `llamavm info [<tag>]` | Show tag, source, git SHA, install path, and install time |
| `llamavm bench <tag> --model <path>` | Benchmark a single version |
| `llamavm bench all --model <path>` | Benchmark every installed version |
| `llamavm doctor` | Diagnose installation and PATH configuration |

Run any subcommand with `--help` for full options.

## How it works

`llamavm install <tag>` clones llama.cpp at the given tag into a staging directory, runs the standard cmake build with Metal enabled, and atomically renames the result into `~/.llamavm/versions/<tag>/`. Failed builds leave no trace in `llamavm list`.

After the build completes, llamavm scans `<version>/source/build/bin/` for executable files matching `llama-*` and drops a small Go shim binary into `~/.llamavm/shims/` for each one — typically ~50 tools per current llama.cpp release (`llama-cli`, `llama-server`, `llama-bench`, `llama-embedding`, `llama-tokenize`, `llama-perplexity`, `llama-mtmd-cli`, ...). Future llama.cpp releases that ship new tools get shimmed automatically with no llamavm change. When invoked, each shim walks up from the current directory looking for `.llama-version`, then falls back to `~/.llamavm/current`, then `exec`s the corresponding binary inside the resolved version's directory. Shim overhead is under 50ms.

`llamavm uninstall <tag>` removes the version and garbage-collects shims that no remaining install provides; shared shims (still backed by another version) are kept. If you upgraded llamavm itself and want a previously-installed version's full shim set, just reinstall that tag — the new shims will land in `~/.llamavm/shims/` regardless of which version is currently active.

## Uninstall

```bash
brew uninstall llamavm
rm -rf ~/.llamavm
```

## License

MIT
