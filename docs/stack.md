# Stack freshness

This audit was verified from vendor release pages and official documentation on
2026-09-18 UTC. The Go modules, workflows, Swift manifests, packaging syntax,
and update automation are intentionally kept separate from product behavior.

## Audit table

| Component | Pinned before | Latest found | Source URL | Bumped to |
| --- | --- | --- | --- | --- |
| Root Go module | `go 1.27` | Go 1.27.1, released 2026-09-01 | https://go.dev/doc/devel/release | `go 1.27.1` |
| Bar Go module | `go 1.27` | Go 1.27.1, released 2026-09-01 | https://go.dev/doc/devel/release | `go 1.27.1` |
| `actions/checkout` | `@v4` | v7.0.1, released 2026-07-20 | https://github.com/actions/checkout/releases/tag/v7.0.1 | `@v7` |
| `actions/setup-go` | `@v5` | v7.0.0, released 2026-07-16 | https://github.com/actions/setup-go/releases/tag/v7.0.0 | `@v7` |
| `actions/upload-artifact` | `@v4` | v7.0.1, released 2026-04-10 | https://github.com/actions/upload-artifact/releases/tag/v7.0.1 | `@v7` |
| `actions/download-artifact` | `@v4` | v8.0.1, released 2026-03-11 | https://github.com/actions/download-artifact/releases/tag/v8.0.1 | `@v8` |
| `setup-go` version file flow | `go-version-file: go.mod` | Supported by setup-go v7 | https://github.com/actions/setup-go#readme | Retained with exact Go 1.27.1 module declarations |
| `ubuntu-latest` | Moving label | Ubuntu 24.04 | https://github.com/actions/runner-images/blob/main/README.md | Retained and annotated |
| `macos-latest` | Moving label | macOS 26 arm64, default Xcode 26.6 | https://github.com/actions/runner-images/blob/main/images/macos/macos-26-arm64-Readme.md | Retained and toolchain-verified |
| `fyne.io/systray` | v1.12.2 | v1.12.2, released 2026-06-09 | https://github.com/fyne-io/systray/releases/tag/v1.12.2 | v1.12.2 retained |
| XcodeGen | Homebrew latest at job time | 2.46.0, released 2026-07-16 | https://github.com/yonaskolb/XcodeGen/releases/tag/2.46.0 | Homebrew latest plus version output |
| Swift package tools | 5.10 | Swift 6.3 | https://www.swift.org/blog/swift-6.3-released/ | 6.3 |
| Swift Xcode language mode | 5.10 | Swift 6 language mode in Xcode 26.6 | https://developer.apple.com/xcode/system-requirements/ | 6.0 in iOS and watchOS targets; macOS package stays on Swift 5 mode due an Xcode 26.6 compiler crash |
| Apple deployment targets | iOS 17, watchOS 10, macOS 13 | Supported by Xcode 26.6 | https://developer.apple.com/xcode/system-requirements/ | Retained as compatibility minimums |
| Homebrew formula and cask | No livecheck blocks | Current service, OS blocks, and livecheck syntax | https://docs.brew.sh/Formula-Cookbook | Added `livecheck` blocks and retained valid blocks |
| Boxdeck tap release | 0.2.0 | v0.2.0, released 2026-09-17 | https://github.com/wicolian/boxdeck/releases/tag/v0.2.0 | 0.2.0 retained |
| Dependabot Swift ecosystem | Not configured | Swift `swift` ecosystem is supported for v5 and v6 | https://docs.github.com/en/code-security/reference/supply-chain-security/dependabot-options-reference | Weekly Swift entries added |
| `softprops` action | None found | Not applicable | https://github.com/softprops/action-gh-release/releases | No action added |
| `gh release` | GitHub CLI command in release workflow | Runner-provided CLI, no repo version pin | https://cli.github.com/manual/gh_release_create | Retained, documented as unpinned |
| `index.html` external assets | No CDN or external script URL | No CDN or external script URL found | https://github.com/wicolian/boxdeck/blob/main/index.html | No change |
| `install.sh` release source | `releases/latest` | `releases/latest` | https://github.com/wicolian/boxdeck/blob/main/install.sh | Retained |

## Apple compatibility

Xcode 26.6 includes Swift 6.3, iOS 26.5, watchOS 26.5, and macOS 26.5 SDKs.
Its supported deployment range includes the existing iOS 17 and watchOS 10
minimums, and the package macOS 13 target remains below its supported macOS
deployment range. These targets are compatibility promises, not stale compiler
pins, so they remain unchanged while the compiler moves to Swift 6. The macOS
bar package uses the Swift 5 language mode explicitly because Xcode 26.6.0's
Swift 6.3.3 frontend crashes while compiling its existing MenuPopover code.

The workflow prints `xcode-select -p`, `xcodebuild -version`, and `swift
--version`. It uses the current stable default on `macos-latest`, rather than a
preview Xcode image or an unverified selector path.

## Homebrew and update automation

The formula keeps `service do`, `on_macos`, and `on_linux` blocks. Formula and
cask livecheck blocks use the documented GitHub latest strategy. The renderer
uses an explicit version placeholder and replaces it from its required release
argument, so it no longer carries a stale 0.1.0 template.

Dependabot checks GitHub Actions, both Go modules, the Android Gradle tree, the
BoxdeckKit Swift package, and the root Swift package weekly. The root Swift
entry is the valid mapping for `Package.swift`, whose executable source path is
`macos/BoxdeckBar`; Dependabot does not accept a directory that lacks a
`Package.swift` manifest.

`scripts/stack-check.sh` curls the same official release sources, prints pinned
and latest values, and fails only when an action or module is behind by a major
version. Patch and minor freshness remains Dependabot's job.
