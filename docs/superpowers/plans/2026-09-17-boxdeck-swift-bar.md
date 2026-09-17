# Boxdeck Swift Menu Bar Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the macOS Go menu bar build with a native SwiftUI MenuBarExtra app that reads the existing bar configuration, renders the Go menu model, refreshes boxes concurrently, supports alerts and controls, and ships as a signed universal `.app`.

**Architecture:** `macos/BoxdeckBar` contains Foundation-only contracts, config persistence, HTTP decoding/client behavior, pure menu-model formatting, and local usage execution so those pieces are testable without AppKit. SwiftUI views consume an observable refresh model and perform UI actions through injected methods. A package-local app entry point supplies the menu bar extra and Settings scene, while CI creates `Info.plist`, signs the app ad hoc, and zips the exact Homebrew asset name.

**Tech Stack:** Swift 5.10 language mode, Swift Package Manager, macOS 13+, SwiftUI `MenuBarExtra`, Foundation `URLSession`, UserNotifications, ServiceManagement `SMAppService`, XCTest, GitHub Actions `macos-latest`.

**Spec:** `/home/ubuntu/codes/boxdeck/.work/phase5/PROMPT-swift.md`

## Global Constraints

- Use the existing `~/.config/boxdeck/bar.json` schema and port behavior from `internal/barclient`.
- The app targets macOS 13+ and uses SwiftUI `MenuBarExtra` with `.menuBarExtraStyle(.window)`.
- Refresh each configured box in parallel with a five second request timeout and never block the UI.
- Notifications are disabled unless `notify: true`; rate limit each box to one notification per five minutes.
- Optional `/api/alerts` and `/api/alerts/disarm` routes are best-effort and do not make refresh fail.
- Build and test on GitHub macOS CI because this Linux checkout has no Swift compiler.
- Use no external Swift dependencies.
- Do not add middle dots, em dashes, or en dashes to source, docs, reports, or PR text.

### Task 1: Package and test fixtures

**Files:**
- Create: `Package.swift`
- Create: `macos/BoxdeckBar/BoxdeckBarCore.swift`
- Create: `macos/BoxdeckBarTests/BoxdeckBarCoreTests.swift`
- Copy: `macos/BoxdeckBarTests/Fixtures/*.json`

- [ ] Write fixture-backed decoding and config/model tests first.
- [ ] Run the macOS workflow test command and confirm the new tests fail because the target does not exist.
- [ ] Add the package manifest and Foundation models for config, state, usage, boxes, peers, alerts, menu lines, and icon state.
- [ ] Run `swift test` on CI and confirm fixture decoding and pure model tests pass.

### Task 2: Config, client, local usage, and menu model

**Files:**
- Create: `macos/BoxdeckBar/ConfigStore.swift`
- Create: `macos/BoxdeckBar/BoxdeckClient.swift`
- Create: `macos/BoxdeckBar/MenuModel.swift`
- Modify: `macos/BoxdeckBarTests/BoxdeckBarCoreTests.swift`

- [ ] Add failing tests for default config, missing config, the Go menu text, quota attention, tailnet labels, and `unreachable since HH:MM` formatting.
- [ ] Implement validated config loading and atomic 0600 writes at the standard macOS path.
- [ ] Implement HTTP GET/POST requests with bearer auth, five second request timeout, optional usage, discovery and alert endpoints, and local `boxdeck usage --json` execution.
- [ ] Implement concurrent configured-box refresh and optional discovered-box merging without retaining credentials in models.
- [ ] Implement deterministic menu model construction and text rendering used by `--print`.
- [ ] Run the focused test suite, then the full Swift test suite on CI.

### Task 3: SwiftUI app, settings, controls, and notifications

**Files:**
- Create: `macos/BoxdeckBar/BoxdeckBarApp.swift`
- Create: `macos/BoxdeckBar/AppModel.swift`
- Create: `macos/BoxdeckBar/MenuPopover.swift`
- Create: `macos/BoxdeckBar/SettingsView.swift`
- Create: `macos/BoxdeckBar/NotificationController.swift`
- Create: `macos/BoxdeckBar/LaunchAtLogin.swift`

- [ ] Add tests for quiet-hour inclusion, disarm state, and notification transition/rate-limit decisions.
- [ ] Implement an observable model whose timer starts at launch, refreshes off the main actor, and publishes only completed snapshots.
- [ ] Implement the monochrome SF Symbol menu bar label, Needs you alerts section, configured and discovered cards, local usage, refresh/settings/launch-at-login/quit controls, and Add a box form.
- [ ] Implement URL opening, alert Ack and first action behavior, quiet-hours and disarm actions, notification categories/actions, and `SMAppService.mainApp` status.
- [ ] Run `swift test` and a release build on CI.

### Task 4: Packaging and release workflow

**Files:**
- Create: `.github/workflows/macos-bar.yml`
- Modify: `packaging/macos/Info.plist`
- Modify: `packaging/macos/build.sh`
- Modify: `.github/workflows/release.yml`
- Modify: `scripts/tap/update.sh`

- [ ] Add workflow dispatch and `phase5-swift` push triggers, Swift build/test, universal binary build, plist copy, ad-hoc signing, zip creation, artifact upload, and bundle inspection.
- [ ] Make the app bundle executable `BoxdeckBar`, bundle id `dev.wicolian.boxdeck-bar`, and `LSUIElement` true.
- [ ] Replace only the macOS Go bar release job while keeping Linux and Windows Go jobs and asset names intact.
- [ ] Keep the existing cask URL and exact `boxdeck-bar_darwin_universal.zip` output.
- [ ] Run the workflow, download and inspect the artifact, and use its URL as CI evidence.

### Task 5: Documentation, PR evidence, and final gates

**Files:**
- Modify: `README.md`
- Create: `/home/ubuntu/codes/boxdeck/.work/phase5/reports/swift.md`
- Create: `/home/ubuntu/codes/boxdeck/.work/phase5/reports/swift.done`

- [ ] Update the Menu bar section to identify Swift on macOS, Go on Linux and Windows, and retain the Homebrew cask command.
- [ ] Rebase onto `origin/main`, run Go tests/vet plus all available Swift and static checks, and sweep tracked task files for banned characters.
- [ ] Switch GitHub auth to `wicolian`, push `phase5-swift`, open a PR against `main`, attach the required text before/after block and CI run URL, then switch auth back to `koushik-databrain`.
- [ ] Download the CI artifact, validate `Contents/Info.plist` with Python plistlib and `Contents/MacOS/BoxdeckBar`, and record commands and exit codes in the report.
- [ ] Write the report completely, verify it, then create the literal `.done` marker last.

