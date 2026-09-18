class Boxdeck < Formula
  desc "Live console for a remote dev box"
  homepage "https://github.com/wicolian/boxdeck"
  version "0.4.1"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/wicolian/boxdeck/releases/download/v#{version}/boxdeck_darwin_arm64"
      sha256 "badef84c074cdc3d805b7f68bebb8e50f521a4cd17a5bed6de2f59dc8fecb968"
    else
      url "https://github.com/wicolian/boxdeck/releases/download/v#{version}/boxdeck_darwin_amd64"
      sha256 "a84e78757d670356ee7baa1992f32ab75cf6ae071e828dee0d0f62569aa5c2be"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/wicolian/boxdeck/releases/download/v#{version}/boxdeck_linux_arm64"
      sha256 "6bea8ffa880f1a4028e8a2667101b69aeac189c5b327756ae86478050ee863d4"
    else
      url "https://github.com/wicolian/boxdeck/releases/download/v#{version}/boxdeck_linux_amd64"
      sha256 "5fc141169a920acd897165242cf4454261c524a18c3071d2cfabc75923234e30"
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
