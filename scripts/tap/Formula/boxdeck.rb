class Boxdeck < Formula
  desc "Live console for a remote dev box"
  homepage "https://github.com/wicolian/boxdeck"
  version "0.2.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/wicolian/boxdeck/releases/download/v#{version}/boxdeck_darwin_arm64"
      sha256 "5633d8674d7ee7e163cd96692d786b878a0be9411f8b8abbc829437faeca43ee"
    else
      url "https://github.com/wicolian/boxdeck/releases/download/v#{version}/boxdeck_darwin_amd64"
      sha256 "bb56a761910807498d7ec9d18fd6fdda65f70efaf29bca2d3d887e070bb99d2f"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/wicolian/boxdeck/releases/download/v#{version}/boxdeck_linux_arm64"
      sha256 "d8cbde4acb554791080e072bd77d8f4c5b3adb895fcae11076fb6e05ccfd5187"
    else
      url "https://github.com/wicolian/boxdeck/releases/download/v#{version}/boxdeck_linux_amd64"
      sha256 "84f3e3989854e8dc967bc969ad2a74f9555fcb7ee391a2d492eae396ffdad12f"
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
