class Boxdeck < Formula
  desc "Live console for a remote dev box"
  homepage "https://github.com/wicolian/boxdeck"
  version "0.4.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/wicolian/boxdeck/releases/download/v#{version}/boxdeck_darwin_arm64"
      sha256 "42ab2c4d72dfdaaf5918c954e4917d7f11a24e559582c38b3a3b425bf489671d"
    else
      url "https://github.com/wicolian/boxdeck/releases/download/v#{version}/boxdeck_darwin_amd64"
      sha256 "1495583cbd0ea5570d1014525340e01989233ad8e23c08015d0f0733234b23ce"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/wicolian/boxdeck/releases/download/v#{version}/boxdeck_linux_arm64"
      sha256 "2ae8a370f09105d2b631480129138c9cb674443b26e3078b15123b722c00cb56"
    else
      url "https://github.com/wicolian/boxdeck/releases/download/v#{version}/boxdeck_linux_amd64"
      sha256 "790c4822f0e2338b1f1a959dcc64d00cc48341ebfbec44ef3f797b9b88d41c38"
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
