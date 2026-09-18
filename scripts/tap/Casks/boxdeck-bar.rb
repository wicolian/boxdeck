cask "boxdeck-bar" do
  version "0.4.1"
  sha256 "d489d65d570c343430d6c2a2fa2d84f8f0d06b2af6ea0f03eaff272622a78a22"

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
