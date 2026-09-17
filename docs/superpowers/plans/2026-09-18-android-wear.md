# Android and Wear OS Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Android phone and Wear OS clients for boxdeck with a shared Kotlin API core, alert actions, device pairing, live updates, notifications, and CI-built debug APKs.

**Architecture:** `android/core` is a pure JVM Kotlin library containing serializable API models, an OkHttp client, SSE event handling, menu model logic, and quiet-hours logic. `android/app` owns Android persistence, Compose screens, CameraX and ML Kit pairing, WebView login, notifications, and UnifiedPush intake. `android/wear` is a standalone Wear Compose app with a Data Layer bridge, tile, and complication, sharing the core models and client.

**Tech Stack:** Kotlin 2.x, Gradle wrapper, AGP 8.x, compileSdk 35, minSdk 26 for the phone and 30 for Wear, kotlinx.serialization, OkHttp, Jetpack Compose Material 3, CameraX, ML Kit barcode scanning, AndroidX security crypto, Wear Compose, Horologist tiles, and GitHub Actions.

**Spec:** `/home/ubuntu/codes/boxdeck/.work/phase5/PROMPT-android.md`, with API behavior from `API.md`, alert contracts from `PROMPT-alerts.md`, and pairing and screen alignment from `PROMPT-ios.md`.

## Global Constraints

- Do not edit `/home/ubuntu/codes/boxdeck`; all source edits are in this worktree.
- Use the dark bridge tokens hull `#0b0e13`, deck `#10141b`, ink, amber `#e8a33d`, moss, and rust; no gradients and no icon soup.
- Keep credentials out of reports, screenshots, and committed files.
- Support `boxdeck://add?url=&token=`, `boxdeck://box/<name>`, and `boxdeck://alert/<id>`.
- Sweep all changed text for `·`, `—`, and `–` before the done marker.
- The CI workflow must run core tests and lint, assemble both debug APKs, and upload both APKs.

### Task 1: Gradle workspace and pure core module

**Files:**
- Create: `android/settings.gradle.kts`, `android/build.gradle.kts`, `android/gradle.properties`, `android/gradle/wrapper/gradle-wrapper.properties`, `android/gradlew`, `android/gradlew.bat`, `android/gradle/wrapper/gradle-wrapper.jar`
- Create: `android/core/build.gradle.kts`
- Create: `android/core/src/main/kotlin/dev/wicolian/boxdeck/core/Models.kt`, `BoxdeckClient.kt`, `MenuModel.kt`, `QuietHours.kt`
- Create: `android/core/src/test/kotlin/dev/wicolian/boxdeck/core/CoreTest.kt`
- Create: `android/core/src/test/resources/testdata/state.json`, `usage.json`, `usage-all.json`, `boxes.json`, `peers.json`

**Interfaces:**
- `BoxdeckClient(baseUrl: String, token: String, httpClient: OkHttpClient = default)` exposes suspend methods for every read and action endpoint in `API.md`, plus `events(lastEventId, listener)` and `/api/net/peers`.
- `BoxdeckModels` use nullable/default fields so the core can decode both the current server and the alert branch after merge.
- `buildMenuModel(boxes)` ports the Go bar model and `QuietHours.isQuiet(now, from, to)` matches the server's midnight-crossing behavior.

- [ ] Add the Gradle settings, plugin versions, repositories, wrapper, and pure JVM core dependencies.
- [ ] Port the Go bar fixtures and decoding types into Kotlin serialization models.
- [ ] Implement authenticated GET, POST, PUT, and SSE methods with bounded timeouts and typed error responses.
- [ ] Implement menu model, quota attention, needs-you classification, quiet-hours logic, and tailnet peer decoding.
- [ ] Add unit tests for fixtures, endpoint request shapes, menu rendering, and quiet hours.
- [ ] Run static Kotlin formatting and inspect the core source for missing API paths.
- [ ] Commit `feat: add shared android boxdeck core`.

### Task 2: Phone Android application shell and storage

**Files:**
- Create: `android/app/build.gradle.kts`, `android/app/proguard-rules.pro`, `android/app/src/main/AndroidManifest.xml`
- Create: `android/app/src/main/kotlin/dev/wicolian/boxdeck/App.kt`, `MainActivity.kt`, `AppTheme.kt`, `BoxStore.kt`, `MainViewModel.kt`
- Create: `android/app/src/main/res/values/strings.xml`, `themes.xml`, `android/app/src/main/res/xml/backup_rules.xml`

**Interfaces:**
- `BoxStore` persists box name, URL, and token JSON using `EncryptedSharedPreferences` and exposes a StateFlow of configured boxes. Refresh merges `/api/boxes` deck cards with `/api/net/peers` from the first reachable deck so plain Tailscale devices are included too.
- `MainViewModel` owns selected tab, selected box, alert list, box snapshots, usage, refresh state, and foreground event collection.
- Notification actions are routed back through the same `BoxdeckClient` instances as inline UI actions.

- [ ] Configure Compose Material 3, Navigation Compose, lifecycle, security crypto, WebView, CameraX, ML Kit, and notification dependencies.
- [ ] Register internet, camera, notification, UnifiedPush, and `boxdeck://` intent filters for add, box, and alert routes.
- [ ] Implement encrypted box storage, refresh orchestration, foreground SSE lifecycle, and deterministic device ordering: local deck, decks, other online devices, offline devices.
- [ ] Implement alert action dispatch with defensive handling for unknown action paths.
- [ ] Create notification channels and notification action plumbing.
- [ ] Commit `feat: add android phone shell and secure box storage`.

### Task 3: Phone Compose screens

**Files:**
- Create: `android/app/src/main/kotlin/dev/wicolian/boxdeck/ui/MainScreen.kt`, `AlertScreen.kt`, `BoxesScreen.kt`, `BoxDetailScreen.kt`, `UsageScreen.kt`, `SettingsScreen.kt`, `QrScannerScreen.kt`, `WebViewScreen.kt`

**Interfaces:**
- `MainScreen` provides bottom navigation for Needs you, Boxes, Usage, and Settings.
- `BoxDetailScreen` provides Overview, Agents, Ports, Usage, Apps, Terminal, and Files tabs.
- All alert action chips call `MainViewModel.performAction`; no screen constructs raw authorization headers.

- [ ] Implement the bridge theme, monospace numeric text, bottom navigation, pull-to-refresh, and empty-state copy.
- [ ] Render open alerts newest first with the required inline actions and alert links.
- [ ] Render box cards, health, agent status, ports, usage bars, and app start/stop controls. Render plain tailnet devices with name, OS, online state, last seen, and `install boxdeck` or `phone` guidance.
- [ ] Render agent live tail and prompt form using `/api/herd/read` and `/api/herd/prompt`.
- [ ] Implement ports as external links and app, terminal, and file actions.
- [ ] Implement authenticated WebView login by posting the user's credentials to `/login`, preserving its cookie, and loading `/term/` or `/files/`.
- [ ] Implement URL/token add flow and CameraX plus ML Kit QR scanning for the exact `boxdeck://add` scheme.
- [ ] Implement settings for notification toggle, quiet hours, disarm, and deck launch.
- [ ] Commit `feat: add android boxdeck screens and pairing`.

### Task 4: Wear OS app, tile, complication, and Data Layer

**Files:**
- Create: `android/wear/build.gradle.kts`, `android/wear/src/main/AndroidManifest.xml`
- Create: `android/wear/src/main/kotlin/dev/wicolian/boxdeck/wear/WearActivity.kt`, `WearApp.kt`, `WearStore.kt`, `WearDataLayerService.kt`, `BoxdeckTileService.kt`, `BoxdeckComplicationService.kt`

**Interfaces:**
- Wear screens read the same core `BoxSnapshot` and `Alert` models and can operate with locally synced boxes.
- Data Layer messages carry redacted box configuration only over the local paired-device channel; tokens remain protected in the phone and Wear secure storage.
- Tile content is a needs-you count, box name, CPU, and agent count with a 15 minute refresh request.

- [ ] Configure Wear Compose, Wear Material 3, play-services wearable, and Horologist tile dependencies with minSdk 30.
- [ ] Implement the Needs you list with large Approve, Snooze, Ack, and alert action chips.
- [ ] Implement the Boxes glance page with CPU, memory, agents, and quota for decks plus OS, online state, and last seen for every synced tailnet device.
- [ ] Implement phone-to-watch box synchronization and standalone local refresh.
- [ ] Implement the tile and complication data source with amber attention state.
- [ ] Register watch notification actions for Approve and Snooze.
- [ ] Commit `feat: add wear needs-you app and glance surfaces`.

### Task 5: UnifiedPush, documentation, and CI

**Files:**
- Create: `android/app/src/main/kotlin/dev/wicolian/boxdeck/push/UnifiedPushReceiverService.kt`
- Create: `android/PUSH.md`, `docs/android.md`, `.github/workflows/android.yml`
- Modify: `README.md`

**Interfaces:**
- Push payloads are alert JSON plus `actions`; the receiver posts a local notification through the same notification builder used by foreground SSE.
- CI runs `:core:test`, `:app:lint`, `:wear:lint`, `:app:assembleDebug`, and `:wear:assembleDebug`, then uploads `app-debug.apk` and `wear-debug.apk`.

- [ ] Add UnifiedPush connector registration and a receiver that accepts ntfy distributor payloads without storing secrets.
- [ ] Document ntfy setup, distributor selection, payload shape, and the future FCM relay in `PUSH.md`.
- [ ] Document screen inventory and `adb install app-debug.apk` sideloading in `docs/android.md`.
- [ ] Add workflow dispatch and `push` on `phase5-android`, Android SDK setup, Gradle cache, lint, tests, builds, and artifacts.
- [ ] Run banned-character and credential sweeps.
- [ ] Commit `ci: build android and wear artifacts`.

### Task 6: Final verification, rebase, PR, and handoff

**Files:**
- Create: `/home/ubuntu/codes/boxdeck/.work/phase5/reports/android.md`
- Create: `/home/ubuntu/codes/boxdeck/.work/phase5/reports/android.done`
- Modify: `/home/ubuntu/codes/boxdeck/.remember/remember.md`

- [ ] Fetch and rebase onto `origin/main` before final checks.
- [ ] Push `phase5-android`, run `android.yml`, and inspect failed logs if needed.
- [ ] Record the green workflow URL and exact artifact names in the PR and report.
- [ ] Add a PR before and after block that explicitly states no emulator is available and describes every phone and Wear screen in words.
- [ ] Open the PR against `main` as `wicolian`, then restore `koushik-databrain` auth.
- [ ] Write the report, verify it, then create the literal `.done` marker last.
- [ ] Save a concise next-session handoff to `.remember/remember.md`.
