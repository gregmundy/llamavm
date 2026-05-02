# Reference Homebrew formula for llamavm.
#
# This is the canonical template that lives in this repo per PRD §5.3.
# The deployed formula lives at github.com/gregmundy/homebrew-tap.
#
# After cutting a release:
#   1. Replace VERSION below with the released semver (e.g. 1.0.0).
#   2. Replace SHA256_PLACEHOLDER with the sha256 of the darwin-arm64 tarball
#      (read from dist/checksums.txt produced by GoReleaser).
#   3. Copy this file to the tap repo at Formula/llamavm.rb and commit.
class Llamavm < Formula
  desc "Version manager for llama.cpp on Apple Silicon"
  homepage "https://github.com/gregmundy/llamavm"
  version "VERSION"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/gregmundy/llamavm/releases/download/v#{version}/llamavm_#{version}_darwin_arm64.tar.gz"
      sha256 "SHA256_PLACEHOLDER"
    end
  end

  def install
    bin.install "llamavm"
    bin.install "llamavm-shim"
  end

  def caveats
    <<~EOS
      llamavm needs its shims directory on PATH. Add this to your shell rc:

        export PATH="$HOME/.llamavm/shims:$PATH"

      Then install a llama.cpp version:

        llamavm install latest
        llamavm doctor

    EOS
  end

  test do
    assert_match "llamavm version", shell_output("#{bin}/llamavm --version")
  end
end
