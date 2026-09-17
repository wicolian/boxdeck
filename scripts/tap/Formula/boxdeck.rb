class Boxdeck < Formula
  desc "Live console for a remote dev box"
  homepage "https://github.com/wicolian/boxdeck"
  version "0.1.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/wicolian/boxdeck/releases/download/v#{version}/boxdeck_darwin_arm64"
      sha256 "REPLACE_DARWIN_ARM64"
    else
      url "https://github.com/wicolian/boxdeck/releases/download/v#{version}/boxdeck_darwin_amd64"
      sha256 "REPLACE_DARWIN_AMD64"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/wicolian/boxdeck/releases/download/v#{version}/boxdeck_linux_arm64"
      sha256 "REPLACE_LINUX_ARM64"
    else
      url "https://github.com/wicolian/boxdeck/releases/download/v#{version}/boxdeck_linux_amd64"
      sha256 "REPLACE_LINUX_AMD64"
    end
  end

  def install
    bin.install Dir["boxdeck*"][0] => "boxdeck"
  end

  service do
    run [opt_bin/"boxdeck", "serve"]
    keep_alive true
  end
end
