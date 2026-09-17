cask "boxdeck-bar" do
  version "0.2.0"
  sha256 "2415fd83638adabcfbb05eed856041a28f0153cdc0ed82baf1796a72e0bc76f0"

  url "https://github.com/wicolian/boxdeck/releases/download/v#{version}/boxdeck-bar_darwin_universal.zip"
  name "boxdeck-bar"
  desc "Cross-platform menu bar client for boxdeck"
  homepage "https://github.com/wicolian/boxdeck"

  app "boxdeck-bar.app"
end
