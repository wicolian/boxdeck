# Agent Usage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add local Claude and Codex usage scanning, quota reporting, saved device history, remote box aggregation, CLI output, and a responsive Usage view to boxdeck.

**Architecture:** `usage.go` owns provider-neutral JSONL parsing, incremental file state, daily rollups, quota samples, persistence, and background scheduling. `pricing.go` owns model rates and config overrides. The HTTP app exposes local usage and aggregate box usage without blocking requests, while the existing single-page deck reads both endpoints on its existing state tick. The direct `usage` command scans configured local roots without starting a server.

**Tech Stack:** Go 1.27 standard library, embedded HTML/CSS/JavaScript, JSONL fixtures, `go test`, `go vet`, cross compilation, agent-browser screenshots.

**Spec:** `/home/ubuntu/codes/boxdeck/.work/usage/PROMPT.md`

## Global Constraints

- Keep Go stdlib only and one binary.
- Work only on branch `usage`; never push or merge `main`.
- Do not print or persist credentials, tokens, passwords, or OAuth secrets.
- Read Claude OAuth credentials only from `~/.claude/.credentials.json`; never refresh expired credentials.
- Store local usage at `~/.local/share/boxdeck/usage.json` with mode 0600 and atomic replacement.
- Keep scans nonblocking for HTTP requests and sample Claude no more than once per five minutes.
- Use UTC day rollups and include the local day in API output.
- Label estimated costs as list-price estimates and label unknown prices as `no price set`.
- Do not write middle dots, em dashes, or en dashes in source, docs, reports, or PR text.

### Task 1: Establish failing scanner and pricing tests

**Files:**
- Create: `usage_test.go`
- Create: `testdata/usage/claude-session.jsonl`
- Create: `testdata/usage/codex-session.jsonl`
- Create: `testdata/usage/codex-growing.jsonl`
- Create: `testdata/usage/credentials.json`
- Create: `pricing.go`
- Create: `usage.go`

**Interfaces:**
- Tests define `scanUsageRoots`, `usageStore`, `usageSnapshot`, `modelUsage`, and `pricingFor` behavior before production implementation.

- [ ] **Step 1: Write fixtures and tests for provider parsing.** Include duplicate Claude message IDs with growing usage, a Claude timestamp and model, a final Codex `token_count` record with cumulative totals and rate limits, a null model quota window, and a file that is scanned once then grows.
- [ ] **Step 2: Run `go test ./... -run 'TestUsage|TestPricing'`.** Confirm the new tests fail because the scanner and pricing interfaces do not exist.
- [ ] **Step 3: Add the minimal pricing table and scanner types.** Keep model prices explicit for the requested Claude and Codex names, use zero rates for unavailable public prices, and preserve an unknown-price marker.
- [ ] **Step 4: Run the focused tests again.** Confirm the remaining failures describe missing parsing behavior rather than test or fixture errors.
- [ ] **Step 5: Commit the test and fixture skeleton.** Use `git add usage_test.go testdata/usage pricing.go usage.go && git commit -m "test: define local agent usage contracts"`.

### Task 2: Implement incremental ledgers, rollups, persistence, and quotas

**Files:**
- Modify: `usage.go`
- Modify: `pricing.go`
- Modify: `config.go`
- Modify: `usage_test.go`
- Create: `testdata/usage/claude-credentials-expired.json`

**Interfaces:**
- `newUsageService(cfg config) *usageService`
- `(*usageService).snapshot(days int) usageResponse`
- `(*usageService).scan(ctx context.Context, now time.Time) error`
- `scanUsageRoots(claudeRoot, codexRoot string, pricing pricingConfig, now time.Time) (usageStore, error)`
- `loadUsageStore(path string) (usageStore, error)` and `saveUsageStore(path string, store usageStore) error`

- [ ] **Step 1: Add config pricing decoding and defaults.** Accept `pricing.model.in`, `cachedIn`, `cacheWrite`, and `out`, while retaining sane zero-price defaults for models whose public price is unavailable.
- [ ] **Step 2: Implement platform-aware roots.** Resolve `CODEX_HOME` first, otherwise `~/.codex`, and resolve `.claude` and `.codex` below the user home on Unix, Darwin, and Windows. Walk only JSONL session files.
- [ ] **Step 3: Implement Claude parsing.** Read assistant records, dedupe by message ID using the last copy, account for input, cached input, cache write, and output tokens, use message timestamps and models, and parse OAuth quota only when credentials exist and the token is not expired.
- [ ] **Step 4: Implement Codex parsing.** Use the last `event_msg` token count per rollout file, retain the session model from `session_meta` or first `turn_context`, keep the latest non-null rate limits from the last 24 hours, and treat API-key null rate limits as a valid no-quota state.
- [ ] **Step 5: Add incremental offsets and restart recovery.** Persist each file path, size, offset, and dedupe or final-ledger state. Rescan only changed files, perform a complete rebuild when the store is missing, and never double count a growing file.
- [ ] **Step 6: Add UTC and local day rollups.** Store daily provider and model rows for at least 30 days, session counts, per-model totals, cost, and unknown-price markers. Compute today's local day and API day values from the same source totals.
- [ ] **Step 7: Add atomic persistence and quota history.** Save to the configured device path with a mode 0600 temporary file and rename. Keep one quota sample per provider window per five minutes for 30 days and preserve the last good Claude sample on expired-token or network errors.
- [ ] **Step 8: Add a nonblocking scheduler.** Scan every 60 seconds while watched and every 10 minutes otherwise; poll Claude at most every five minutes with a five second timeout. Return the last snapshot immediately while a scan is in flight.
- [ ] **Step 9: Add tests for growing files, dedupe, day boundaries, atomic write mode, pricing overrides, null quota windows, expired credentials, and quota retention.** Run `go test ./... -run 'TestUsage|TestPricing'` and then `go test ./...`.
- [ ] **Step 10: Commit the scanner.** Use `git add usage.go pricing.go config.go usage_test.go testdata/usage && git commit -m "feat: scan and persist agent usage"`.

### Task 3: Expose local usage, all-box usage, and control commands

**Files:**
- Modify: `main.go`
- Modify: `boxes.go`
- Modify: `ctl.go`
- Modify: `API.md`
- Modify: `usage_test.go`
- Modify: `api_phase2_test.go`

**Interfaces:**
- `GET /api/usage?days=N` returns providers, device, and updatedAt with the requested response shape.
- `GET /api/usage/all` returns a local-first box breakdown and provider totals.
- `boxCard.Usage` carries each box's today's usage and quota.
- `boxdeck ctl usage [--table]` requests `/api/usage`; `boxdeck usage [--days N] [--json]` uses local roots directly.

- [ ] **Step 1: Add authenticated HTTP routes.** Reject non-GET usage requests with the existing JSON method response, clamp `days` to 1 through 30, and use the cached snapshot without waiting for scans.
- [ ] **Step 2: Extend box fetches.** Fetch `/api/state` and `/api/usage` per configured box with the existing parallel three second timeout and cache. Keep remote tokens server-side and include a compact usage and quota object in each card.
- [ ] **Step 3: Implement `/api/usage/all`.** Sum local and reachable remote provider day and model totals, include per-box details, preserve local first ordering, and surface unreachable boxes without fabricating totals.
- [ ] **Step 4: Extend ctl parsing and tables.** Accept `usage` with no positional arguments, request the route, and render provider, today, seven-day, thirty-day, and model rows without exposing credentials.
- [ ] **Step 5: Implement the direct local `usage` command.** Parse `--days N`, `--json`, and `--table`, scan the local roots without a server, and print only usage values and the `estimate at list price` label.
- [ ] **Step 6: Add route, method, days, table, local command, and aggregate tests.** Run `go test ./...` and verify JSON never contains configured tokens or credential content.
- [ ] **Step 7: Commit the API and CLI.** Use `git add main.go boxes.go ctl.go API.md usage_test.go api_phase2_test.go && git commit -m "feat: expose agent usage APIs and commands"`.

### Task 4: Add the Usage deck view and masthead quota pills

**Files:**
- Modify: `index.html`
- Modify: `API.md`

**Interfaces:**
- The rail includes Usage between Agents and Terminal.
- Every view masthead shows linked Claude and Codex quota pills, including reset time on hover.
- Usage renders only from `/api/usage` and `/api/usage/all` and uses the existing refresh tick.

- [ ] **Step 1: Add the Usage markup.** Include provider cards, quota bars, reset countdowns, 30-day stacked token chart, today, seven-day, and thirty-day totals, model rows, device rows, total row, and the specified empty state.
- [ ] **Step 2: Add the data rendering functions.** Fetch usage on initial load and when the existing refresh tick runs, render null quota windows as unavailable, show `Codex api key` for no Codex quota, and display list-price estimates and no-price markers.
- [ ] **Step 3: Add responsive styling.** Keep the existing amber accent and border-only style, wrap pills below the title on phones, use a full-width chart on phones, and convert model tables to lists below 760px.
- [ ] **Step 4: Add tests or static assertions for the new route labels, empty copy, no-gradient rule, and forbidden punctuation.** Run `go test ./...` and a targeted text sweep.
- [ ] **Step 5: Commit the UI.** Use `git add index.html API.md && git commit -m "feat: add usage view and quota pills"`.

### Task 5: Document usage, run live QA, and capture screenshots

**Files:**
- Modify: `README.md`
- Modify: `API.md`
- Create: `captures/usage-desktop.png`
- Create: `captures/usage-phone.png`
- Create: `captures/usage-overview-pills.png`
- Create: `.work/usage/reports/usage.md`

- [ ] **Step 1: Add the README Usage section.** Explain the CodexBar comparison, local-only data handling, list-price estimate caveat, direct command, pricing override, and screenshot links.
- [ ] **Step 2: Build a QA binary and isolated config.** Copy the existing config to the requested temporary QA path, set `mirror` false, `terminal` false, and `filesPort` zero, and keep generated credentials out of reports.
- [ ] **Step 3: Run the server on port 8103.** Hit authenticated `/api/usage` and `/api/usage/all`, record only trimmed non-secret response fields, and paste the trimmed real `/api/usage` response into `API.md`. Run `boxdeck usage --table` and paste its real output into `README.md`.
- [ ] **Step 4: Use one named agent-browser session with `AGENT_BROWSER_IDLE_TIMEOUT_MS=0`.** Capture the Usage view at 1440x1000 and 390x844, and Overview with masthead pills. Inspect every screenshot before retaining it, close the browser session, and include the exact evidence paths in the report.
- [ ] **Step 5: Stop the QA process and remove only the temporary QA config and generated credential files.** Verify port 8103 is closed and no token or password appears in tracked files, captures, report, or shell output saved for the task.
- [ ] **Step 6: Run the final checks.** Execute `go vet ./...`, `go test ./...`, and builds for `linux/amd64`, `linux/arm64`, `darwin/arm64`, and `windows/amd64`. Run a Unicode punctuation sweep for the forbidden middle dot, em dash, and en dash over all changed text and source files and fix every match.
- [ ] **Step 7: Write the report.** Include scope, commits, test and build exit codes, live route evidence, CLI evidence, screenshot paths, cleanup, punctuation sweep, and any environment-limited evidence. Use honest PASS or BLOCKED-ENV labels.
- [ ] **Step 8: Push and open the PR.** Switch GitHub auth to `wicolian`, push `usage`, create the PR with `gh pr create --base main`, then switch auth back to `koushik-databrain`. Do not merge.
- [ ] **Step 9: Verify the report and branch state, then create the marker last.** Read the final report, confirm the PR and pushed branch, and run `touch /home/ubuntu/codes/boxdeck/.work/usage/reports/usage.done` only after all required evidence is present.

## Coverage review

- Scanner sources, incremental persistence, pricing, quota parsing, and local versus UTC days are covered by Tasks 1 and 2.
- Local API, all-box aggregation, ctl, and serverless command behavior are covered by Task 3.
- Quota pills, Usage navigation, charts, responsive layouts, empty state, and reuse of the state tick are covered by Task 4.
- README and API documentation, live port 8103 QA, CLI output, screenshots, cleanup, cross builds, text sweep, report, PR, and marker ordering are covered by Task 5.
