cask "boxdeck-bar" do
  version "0.3.0"
  sha256 "c11a2547e82b0826d1a50e17a4e56eab026a96afe7e754fb18eb4f65e0dc423a"

  url "https://github.com/wicolian/boxdeck/releases/download/v#{version}/boxdeck-bar_darwin_universal.zip"
  name "boxdeck-bar"
  desc "Cross-platform menu bar client for boxdeck"
  homepage "https://github.com/wicolian/boxdeck"

  livecheck do
    url "https://github.com/wicolian/boxdeck"
    strategy :github_latest
  end

  app "BoxdeckBar.app"
end
