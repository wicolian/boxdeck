cask "boxdeck-bar" do
  version "0.1.0"
  sha256 "REPLACE_BAR_DARWIN"

  url "https://github.com/wicolian/boxdeck/releases/download/v#{version}/boxdeck-bar_darwin_universal.zip"
  name "boxdeck-bar"
  desc "Cross-platform menu bar client for boxdeck"
  homepage "https://github.com/wicolian/boxdeck"

  app "boxdeck-bar.app"
end
