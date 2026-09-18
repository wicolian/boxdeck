class Boxdeck < Formula
  desc "Live console for a remote dev box"
  homepage "https://github.com/wicolian/boxdeck"
  version "0.3.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/wicolian/boxdeck/releases/download/v#{version}/boxdeck_darwin_arm64"
      sha256 "ab002d6250f555f5d68b8513369e374d8389b1a33f5ccdd500e52a316923f340"
    else
      url "https://github.com/wicolian/boxdeck/releases/download/v#{version}/boxdeck_darwin_amd64"
      sha256 "45718bd3646b41b71e55860abf3cdf350e862c30e05a0e56a8355cb82a8cf246"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/wicolian/boxdeck/releases/download/v#{version}/boxdeck_linux_arm64"
      sha256 "c49fbb61c5bd312c92d0242a6a01eaa1f9363efc6ba6ca73014fbdd1154cebdd"
    else
      url "https://github.com/wicolian/boxdeck/releases/download/v#{version}/boxdeck_linux_amd64"
      sha256 "eda4abb09a9cbc01fe1a924f3e00ecf204322d766ab9f5c3e0bd14a38e08b38a"
    end
  end

  def install
    bin.install Dir["boxdeck*"][0] => "boxdeck"
  end

  livecheck do
    url "https://github.com/wicolian/boxdeck"
    strategy :github_latest
  end

  service do
    run [opt_bin/"boxdeck", "serve"]
    keep_alive true
  end
end
