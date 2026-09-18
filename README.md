# boxdeck

A live console for your remote dev box. A single Go binary serves the deck, files
and terminal through one login. The same stream follows you between views.

- **Berths** show listening ports, page titles and working folders. Click a row to
  open the app inside the deck, or use its pop-out button. On a phone, links open a tab.
- **Live health** shows each CPU core, total CPU, memory, swap, load, network rates
  and disk I/O. The browser keeps 120 samples per series.
- **Processes** shows CPU, memory, age, user and folder. Filter the list, sort by
  CPU or memory, then stop a process with an inline confirmation.
- **Agents** show running coding tools, their folders, model and tmux pane.
  Herdr sessions also show their title, status, context use and five-hour limit.
- **herdr**, **tmux**, **browsers** and **Docker** show what is running on the box.
- **Fresh reports** link recent Markdown, HTML, images and PDFs through `/files/`.
- **Files** is a small inbuilt editor with a lazy tree, syntax-aware source view,
  atomic saves, conflict detection and a recoverable trash folder.
- **Git** shows changed files, capped diffs, a client-drawn commit graph and worktrees.

<!-- before-and-after:start -->
| Before: ssh and a terminal | After: boxdeck, live on the same box |
|:---:|:---:|
| ![Before](./captures/before.png) | ![After](./captures/ba/after-overview.png) |

| Processes, sortable, stop from the page | Boxes, many machines in one console |
|:---:|:---:|
| ![Processes](./captures/ba/after-processes.png) | ![Boxes](./captures/ba/after-boxes.png) |

<details><summary>On a phone</summary>

| Phone |
|:---:|
| <img src="./captures/ba/after-phone.png" width="390" alt="boxdeck on a phone"> |

</details>
<!-- before-and-after:end -->

### Phase 5 UX pass

The UX pass groups the rail into Watch, Work, Fleet, and Settings, keeps the
five highest-value destinations on phones, and gives Boxes a device-first view
of every tailnet peer. The same view states are captured at both sizes:

| Desktop overview | Phone overview |
|:---:|:---:|
| ![UX desktop overview](./captures/ux/after/overview-desktop.png) | <img src="./captures/ux/after/overview-phone.png" width="390" alt="boxdeck UX overview on a phone"> |

### Phase 4 remote control surfaces

| Herd | Browser | Network |
|:---:|:---:|:---:|
| ![Herd](./captures/net-after-herd-needs-desktop-final.png) | ![Browser](./captures/net-after-browser-desktop-final.png) | ![Network](./captures/net-after-network-desktop-final.png) |

The Herd and Browser views also have phone layouts in
[`captures/`](./captures/). The Browser view uses the same managed Chromium
process for agent CDP clients and the human canvas.

## Files and Git

| Files before: folder viewer | Files after: editor and tree |
|:---:|:---:|
| ![Files before](./captures/files-open-before.png) | ![Files after](./captures/files-root-after.png) |

Open file editor preview:

![Files open file](./captures/files-open-after.png)

| Git before: existing deck | Git after: Changes with diff |
|:---:|:---:|
| ![Git before](./captures/git-before.png) | ![Git Changes](./captures/git-changes-after.png) |

| Git Graph | Files on a phone |
|:---:|:---:|
| ![Git Graph](./captures/git-graph-after.png) | ![Files phone](./captures/files-phone-after.png) |

No Node runtime, npm packages or Go dependencies. Linux and macOS are both
supported. On Linux, `ss`, `ps`, `tmux`, `docker` and `find` supply optional
machine details and the shared SSE sampler reads `/proc` once per second while
connected, with no `ps` in the one-second loop. On macOS the same views come
from `ps`, `lsof`, `sysctl`, `vm_stat` and `netstat`: agents with their working
directory and model, listening ports with their process, CPU, memory, swap,
load, uptime, network rates and the process list. Per core CPU and disk I/O
rates are Linux only. Results are cached, browser connections stop in a hidden
tab, and the older state health sample slows from 3 to 30 seconds after 15
seconds without a poll. The Claude quota reads the Claude Code login from
`~/.claude/.credentials.json` on Linux and from the login Keychain on macOS.

| Agents on a Mac | Processes on a Mac |
|:---:|:---:|
| ![Agents on macOS](./captures/agents-macos.png) | ![Processes on macOS](./captures/processes-macos.png) |

Current toolchain, runner, and Apple support versions are maintained in
[docs/stack.md](docs/stack.md).

## Navigate the console

Use the collapsible left rail for grouped Watch, Work, Fleet, and Settings
surfaces. Agent controls live from Agents, while Browser, Network, Apps, Files,
Git, Docker, Boxes and Settings remain available in their groups.
Routes use hashes, such as `#/processes`, so
changing views keeps the page and stream alive. On phones, four direct links and
More form a five-item bottom bar. More opens the remaining views.

Apps is the catalog of tools that run on the box. Built-ins include Terminal,
Files, Chromium, T3 Code, herdr, Jev, code-server, File Browser, Ollama and
Syncthing. Each card shows whether its tool is running, detected, or missing;
managed tools have Start, Stop, Restart, Open and Log actions. Add a JSON recipe
to `~/.config/boxdeck/apps` and choose Reload recipes, or use the `apps` array
in the config. See [apps/README.md](apps/README.md).

![Apps grid on desktop](./captures/apps/apps-grid-desktop.png)
![Apps grid on a phone](./captures/apps/apps-grid-phone.png)
![Apps log drawer](./captures/apps/apps-log-drawer.png)
![Apps strip on Overview](./captures/apps/apps-overview-strip.png)

Each board has a filter with a clear action when nothing matches. Ports, Processes,
Agents and Docker also have sort controls. Terminal fills the view. Files has a
two-pane editor and tree; Git opens on Changes with Graph and Worktrees tabs.
Settings shows the running config, masks passwords and box tokens,
and reports only the token count. It also shows the binary version and update hint.

## Usage

The Usage view is inspired by CodexBar's local provider and quota cards. It reads
Claude OAuth quota when a Claude login is already present, and reads Claude and
Codex session ledgers stored on each device. Session content never leaves the
device, and the displayed cost is an estimate at list price, not an invoice.

Use it from a laptop without starting the deck:

```sh
boxdeck usage --days 7 --table
boxdeck usage --days 30 --json
```

How the estimate is made:

- Prices are the providers' own list prices (platform.claude.com and developers.openai.com),
  per million tokens, for input, cached input, cache write and output. Cached input is priced
  at the cache read rate, never at the input rate.
- Codex: a session's `input_tokens` includes its cached reads and cache writes, so the uncached
  part is what is left after both. Each turn is billed on its own: a turn with more than
  272k input tokens gets the long context rate, and the thread's service tier applies as a
  multiplier (`flex` 0.5, `fast` 2, `priority` 2). Rows are keyed `model@tier+long` so you can see
  which tier a cost came from.
- Claude: tokens are already split by the API into input, cache write, cache read and output.
  Claude subscriptions do not bill per token; the number is what the same usage would cost on the
  API, which is the only honest comparison across devices.
- Every response carries `device`, the box's name, so a fleet roll-up says which machine spent what.

Override any price or tier multiplier in the config, USD per million tokens:

```json
{"pricing":{"model":{"gpt-6-astra":{"in":10,"cachedIn":1,"cacheWrite":12.5,"out":50}},"tiers":{"flex":0.5}}}
```

![Usage view on desktop](./captures/usage-desktop.png)
![Usage view on a phone](./captures/usage-phone.png)
![Quota pills on Overview](./captures/usage-overview-pills.png)

Keyboard shortcuts:

| Keys | Action |
| --- | --- |
| `g o` | Overview |
| `g p` | Ports |
| `g r` | Processes |
| `g a` | Agents |
| `g l` | Alerts |
| `g u` | Usage |
| `g t` | Terminal |
| `g v` | Apps |
| `g f` | Files |
| `g g` | Git |
| `g b` | Browser |
| `g d` | Docker |
| `g n` | Network |
| `g x` | Boxes |
| `g s` | Settings |
| `g h` | Agent controls |
| `/` | Focus the current filter |
| `Esc` | Close the viewer or sheet |
| `?` | Show shortcuts |

The Processes table tags agents, browsers and servers. Stop first offers an inline
SIGTERM confirmation. If the process remains, Force stop offers SIGKILL. PID 1,
process groups and the serving boxdeck process are protected. The page also sends
the process start counter so a reused PID cannot redirect a pending action.

The live band uses in-place SVG updates batched with `requestAnimationFrame`.
Per-core bars fit 4 to 64 cores. Process CPU is a percentage of one core; total
CPU is a percentage of the whole machine. Network sums non-loopback interfaces.
Disk rates use whole physical block devices and exclude partitions and virtual
stacking devices. New or reset counters start at zero instead of producing a spike.
Linux supplies these live counters; other platforms keep their existing partial
health support and can use `boxdeck ctl` to control a Linux box.

The UI reads `/api/stream`, `/api/ui/procs` and `/api/ui/settings` and sends process
signals to `/api/proc/kill`. The public token and CLI API is documented in [API.md](./API.md).
Herd reads and steers herdr or tmux panes. Browser manages one Chromium process
and exposes its authenticated CDP tunnel. Network shows Tailscale peers, Serve
entries, local interfaces and mirrors. Boxes uses the real `/api/boxes` response. It keeps the local box first and shows
an unreachable timestamp and a next step when another box does not respond.

## Install

Download the latest tagged release and run the interactive installer:

```sh
curl -fsSL https://raw.githubusercontent.com/wicolian/boxdeck/main/install.sh | sh
```

It chooses Linux or macOS and amd64 or arm64, installs to `~/.local/bin/boxdeck`,
and runs `boxdeck install`. On first install, choose a user, password and hostname.
To connect every device on one tailnet, pass the same private token to every
install: `boxdeck install --fleet-token "$FLEET_TOKEN"`. The token must also be
present in each device's `tokens` list.
On Linux it writes and starts `~/.config/systemd/user/boxdeck.service` with
`Nice=10`, then enables linger. Existing config is kept. On macOS, run `boxdeck serve`
after setup; automatic service installation uses Linux systemd.

The downloader needs a published `v*` release containing the Go binaries. To build
from a checkout with the current Go version in [docs/stack.md](docs/stack.md) or newer:

```sh
make build
./boxdeck version
./boxdeck install
```

Open `http://<your-box>:8100`. The default bind is loopback; use the automatic
Tailscale mirror or an SSH tunnel to reach it. Keep the deck on a private network.

## Menu bar

`boxdeck-bar` is a cross-platform client for monitoring one box or a fleet without
keeping the web deck open. It shows CPU, memory, load, agents, open ports, Claude
and Codex quota, and local usage. A box that needs attention turns the tray icon
amber; an unreachable box turns it rust. The client never executes work on a box.

Install the server and menu bar client on macOS with Homebrew:

```sh
brew install wicolian/tap/boxdeck
brew install --cask wicolian/tap/boxdeck-bar
brew services start boxdeck
```

On Linux, install the server with the installer above, then download
`boxdeck-bar_linux_amd64` or `boxdeck-bar_linux_arm64` from the latest release,
place it on your PATH, and copy `packaging/linux/boxdeck-bar.desktop` to
`~/.config/autostart/`. On Windows, download `boxdeck-bar_windows_amd64.exe`
from the latest release and run it at login.

The menu bar config is `~/.config/boxdeck/bar.json` on macOS and Linux, or
`%APPDATA%\boxdeck\bar.json` on Windows:

```json
{
  "boxes": [{"name": "box", "url": "http://box:8100", "token": "..."}],
  "fleetToken": "...",
  "refreshSec": 30,
  "openWith": "browser",
  "notify": false
}
```

Set `fleetToken` when the net discovery service is enabled. The bar then shows
discovered boxdeck devices below configured boxes with a `tailnet` label. A
server without `/api/net/peers` or discovered box fields is treated as having no
discovery. Keep tokens in this file with mode 0600 and use a private tailnet.

![boxdeck-bar menu model mock](./captures/bar-menu.png)

macOS uses the native SwiftUI BoxdeckBar app. Linux and Windows use the Go
boxdeck-bar app. The macOS app is ad hoc signed. Gatekeeper may require
right-click Open the first time until a notarized build is available.

## Connect an agent

Open Settings, choose Connect an agent, and create a token labelled `agent`.
Then connect Claude Code from any trusted machine:

```sh
claude mcp add --transport http boxdeck http://box:8100/mcp --header "Authorization: Bearer <agent-token>"
```

The MCP server gives an agent tools for box state, processes, agent panes,
alerts, usage, files, Git, apps, browser control and screenshots. The `run`
tool stays hidden until `allowRun` is enabled in the Connect section. Keep the
token on a private tailnet and revoke it with `boxdeck token revoke PREFIX`.
The Connect section also provides Codex, Cursor, Windsurf, curl and stdio
bridge snippets, plus a phone pairing QR.

## Alerts and phone delivery

Alerts watch agent input waits, stuck terminal panes, usage limits, quotas, box
health, watched processes, app exits and shell probes. They are recorded locally
even during quiet hours. Notifications are off by default. Alerts are deduplicated
for 15 minutes and the inbox is capped at 5000 records kept for 30 days.

Native phone push is the first choice. Build and install the iOS app, create an
APNs key, then enter the Key ID, Team ID, bundle ID, environment, and `.p8` file
in the Phone push block in Settings. The deck sends directly to Apple APNs, and
the iPhone and Apple Watch show Approve, Yes, No, Interrupt, Snooze 2h, and Ack.
See [docs/ios-setup.md](docs/ios-setup.md) for the ten setup steps.

Browser push works on any laptop or phone without an app. Open Alerts, choose
Delivery, and select Get alerts on this device. The deck generates its own VAPID
key on first run, so no third party account is involved: the browser's push
service only ever sees an encrypted payload. Notifications show the alert with
Approve, Yes, No, Interrupt, and Snooze 2h buttons, and a tap opens the deck at
the alert. Each button carries the same per alert action token as ntfy. Browser
push needs a secure page: `http://localhost:8100` on the box, or https behind
Tailscale or a reverse proxy. The Delivery tab says so when the page is plain
http. Subscriptions are sealed with the deck secret in
`~/.local/share/boxdeck/webpush.json` (mode 0600) and a browser that
unsubscribes is forgotten on the next send. Nothing is sent while alerts are
disarmed or during quiet hours, and the service worker never caches the deck.

ntfy remains available if you do not want to run the iOS app or a browser.
Install the ntfy app, subscribe to a private topic, then add this to the
boxdeck config:

```json
{
  "sinks": [{
    "name": "phone",
    "type": "ntfy",
    "url": "https://ntfy.sh",
    "topic": "replace-with-a-private-random-topic",
    "token": "",
    "deckURL": "https://your-private-boxdeck-host"
  }]
}
```

Open the ntfy app, tap Subscribe, enter the same private topic, and done. The
Alerts view can test the sink. Critical alerts carry the highest ntfy priority,
the deck link, and action buttons for Approve, Yes, No, Interrupt and Snooze 2h.
Each button carries a token minted for that one alert: it can only run that alert's own
actions (and ack, snooze, resolve) and it expires after 24 hours. The fleet token never
leaves the box. Keep the topic private anyway, and expose boxdeck only on a private
tailnet or VPN.

Alerts view captures:

| Existing overview | Overview with open alert count |
|:---:|:---:|
| ![Alerts before](./captures/alerts/overview-before.png) | ![Alerts overview](./captures/alerts/overview-after.png) |

| Alerts list | Rules | Delivery |
|:---:|:---:|:---:|
| ![Alerts list](./captures/alerts/list.png) | ![Alert rules](./captures/alerts/rules.png) | ![Alert delivery](./captures/alerts/delivery.png) |

Phone layout:

![Alerts on a phone](./captures/alerts/phone-list-cards.png)

Browser push in the Delivery tab, before and after a browser subscribes:

| Get alerts on this device | A subscribed browser |
|:---:|:---:|
| ![Browser push offer](./captures/alerts/delivery-webpush.png) | ![Browser push subscribed](./captures/alerts/delivery-webpush-subscribed.png) |

Shell probes use `bash -lc`, with exit 0 healthy and nonzero or timeout as one
incident. A recovery emits one info event:

```json
{"probes":[{"name":"ci","cmd":"gh run list ...","every":"5m","timeout":"30s"}]}
```

From any terminal on the box, without a token:

```sh
boxdeck alert --source tests --severity critical --title "Tests failed" --body "Open the log"
npm test || boxdeck alert --source tests --severity critical --title "npm test failed"
```

It reads `~/.local/share/boxdeck/inbox-secret` (created by the deck, mode 600, so only your
user can send) and posts to the loopback port with it in `X-Boxdeck-Local`. Other clients
use the normal deck cookie, Basic authentication or a bearer token.

## iPhone and Apple Watch

The Apple targets live under `apple/`. The iPhone app has Needs You, Boxes, Usage,
and Settings tabs. Add a box with its URL and bearer token, or open the deck
Settings view and scan the Pair a phone QR. The Boxes tab includes every peer
reported by Tailscale, including devices without boxdeck, with an install hint.
The watch app has large Needs You actions, a boxes glance, and a complication
that refreshes every 15 minutes.

Native APNs delivery is direct from the deck. The iOS app registers its device
token with every configured deck, and the watch app mirrors the same category
and can register its own token when installed standalone. ntfy remains the
fallback for users who do not want the iOS app. The complete contract is in
[PUSH.md](PUSH.md).

## Android and Wear OS

The Android phone and Wear OS apps put Needs you first, then show every deck and
every Tailscale peer. The Boxes tab merges `/api/boxes` with `/api/net/peers`, so
plain phones and machines appear even when boxdeck is not installed. See
[`docs/android.md`](docs/android.md) for the screen list and debug APK sideload
steps.

For background alerts, install the ntfy Android app, choose it as the UnifiedPush
distributor when Android asks, and configure the same ntfy topic in boxdeck's
ntfy sink. The app renders the alert actions from the server and calls the deck
when one is tapped. See [`android/PUSH.md`](android/PUSH.md).

## One login

The sign-in page sets a signed, HttpOnly, SameSite=Lax cookie for 30 days. Its
32-byte secret lives beside the config in `~/.config/boxdeck/secret`, mode 0600.
Sign out clears the browser cookie. Basic authentication still works for API clients;
unauthenticated API calls receive JSON with status 401, without a browser dialog.

Files live at `/files/`, rooted at your home folder by default. Markdown is rendered
with headings, paragraphs, flat lists, fences, inline code, emphasis, links, images
and tables. Raw Markdown HTML is escaped. HTML and SVG files keep the old sandbox
policy; files send `nosniff`. Paths cannot climb above the configured root. Symlinks
may intentionally point outside it, for example `~/box` pointing to a mount.

Terminal lives at `/term/`. When enabled, boxdeck starts:

```sh
ttyd -p 7681 -i 127.0.0.1 -W --base-path /term tmux new-session -A -s web
```

Install ttyd and tmux to use it. Boxdeck handles authentication and WebSocket traffic;
ttyd has no password of its own and its port is excluded from mirroring. With
`terminal:false`, boxdeck can proxy a separately managed ttyd at `ttydPort`.
The Terminal link dims when the upstream is unavailable.

When migrating an existing ttyd user service, stop it yourself before starting the
Go service, so boxdeck can own port 7681:

```sh
systemctl --user disable --now ttyd.service
```

If the old `files-web.service` owns 8090 and `filesPort` is enabled, stop it too:

```sh
systemctl --user disable --now files-web.service
```

The Go installer replaces `boxdeck.service`. It leaves other services alone.

## Herd and Herdr

Requires [herdr](https://github.com/herdrdev/herdr) 0.9 or newer. Boxdeck reads its
local socket at `~/.config/herdr/herdr.sock` while someone is watching, cached for
3 seconds. The card shows workspaces, tab counts and agent counts by status.
It disappears when herdr or its socket is absent.

Agent rows show the pane title, `working` / `idle` / `blocked` / `done` / `unknown`,
context and five-hour limit when supplied, and the pane ID. Click the row to focus
that pane in herdr on the box. The authenticated `/api/herdr/focus` endpoint sends
herdr's `agent.focus` socket request, equivalent to `herdr agent focus <pane_id>`.
Process entries are matched by their inherited herdr pane ID, not their folder name.

The Herd view expands every visible agent into a live terminal tail. Waiting or
prompted agents float to the top and get a `needs you` tag. Interrupt, Enter,
`y`, `n` and prompt actions use herdr when available, then fall back to tmux.

## Browser and Network

The Browser view starts one managed Chromium or Chrome process per box, lists
its open pages, captures screenshots through CDP, and provides a human-usable
interactive canvas with URL bar, tabs, keyboard, pointer and touch input. From
a remote agent, use:

```sh
AGENT_BROWSER_IDLE_TIMEOUT_MS=0 agent-browser --cdp \
  "http://box:8100/cdp?token=$TOKEN" open https://example.com
```

The Network view reports Tailscale names and IPs, peers, Tailscale Serve
entries, local IPv4 interfaces, mirror ports and bind errors. Mirrored ports
carry no password, so only mirror a port to a trusted private tailnet.
It also probes online tailnet peers on port 8100, marks installed boxdeck
devices with their version, and shows today's usage when the shared fleet token
is accepted. Peers without boxdeck include the one-line install command.

## Config

`~/.config/boxdeck/config.json`:

```json
{
  "user": "me",
  "password": "choose-a-password",
  "host": "box",
  "bind": "127.0.0.1",
  "port": 8100,
  "title": "Deck",
  "filesPort": 0,
  "filesRoot": "~",
  "terminal": true,
  "ttydPort": 7681,
  "fleetToken": "same-private-token-on-every-box",
  "quick": [["App", 3001]],
  "reportRoots": ["~/reports", "~/box"],
  "repoRoots": ["~", "~/codes", "~/src", "~/projects"],
  "reportDays": 3,
  "mirror": "auto",
  "mirrorBind": "",
  "known": {"3001": "My app", "5173": "Vite demo"},
  "hide": [22, 53, 111, 139, 445],
  "agentPattern": "^(\\S*/)?(claude|codex|aider|opencode|goose)(\\s|$)"
}
```

- `host` accepts a hostname or a pasted URL such as `https://box/`. Ports accept
  numbers or strings. The scheme and trailing slash are removed from `host`.
- Files and Terminal shortcuts are included automatically. Old shortcuts named
  Files or Terminal use the new same-origin paths. Other shortcuts dim when closed.
- `filesPort` is only for old links. When greater than zero, that loopback port
  redirects requests to `/files/` on the deck. `filesAuth` remains accepted for
  migration; all files now use the deck login.
- `terminal` defaults to true when ttyd is on PATH. `ttydPort` defaults to 7681.
- `known` labels ports. Other listeners get an HTTP title or a process name.
- `repoRoots` limits Git discovery to depth three. `node_modules` is skipped, and
  report roots plus `~/codes` remain available for discovery.
- Reports outside `filesRoot` remain listed without a link. Title probes cache for
  2 minutes, Docker for 10 seconds and reports for 20 seconds.
- `BOXDECK_CONFIG` chooses a different config file. Existing overrides remain:
  `BOXDECK_PORT`, `BOXDECK_BIND`, `BOXDECK_HOST`, `BOXDECK_USER`, `BOXDECK_PASSWORD`,
  `BOXDECK_FILES_PORT`, `BOXDECK_FILES_ROOT`, `BOXDECK_FILES_AUTH`,
  `BOXDECK_MIRROR`, `BOXDECK_MIRROR_BIND`. Terminal also accepts
  `BOXDECK_TERMINAL` and `BOXDECK_TTYD_PORT`. Use `0` or `false` to disable a boolean.

## Your localhost, from another device

The mirror republishes local listeners on the Tailscale `100.64.0.0/10` address,
using plain TCP for HTTP, WebSockets and HMR. It needs no sudo or `tailscale serve`.
`mirror:auto` enables it when that interface exists; use `false` to disable it or
`mirrorBind` to choose another private address. Hidden ports and the ttyd upstream
are excluded. Other mirrored apps keep their own authentication; the tailnet is
their access boundary. The mirror rescans listeners every 5 seconds even when the
deck is closed. A failed bind shows the port and reason below Berths.

The viewer has a URL, loading state, reload, pop-out and close. Esc closes it.
Files and Terminal use the shared login; other dev servers keep their own origins.
An app that forbids framing can still be opened with the pop-out button.

## Build and release

```sh
go vet ./...
go test ./...
make build
make release
```

`make release` produces `dist/boxdeck_linux_{amd64,arm64}` and
`dist/boxdeck_darwin_{amd64,arm64}` with CGO disabled. A pushed `v*` tag runs the
same build in GitHub Actions and attaches all four binaries to a GitHub Release.

The [Node files in `legacy/`](./legacy/) are frozen for one release.

MIT.
