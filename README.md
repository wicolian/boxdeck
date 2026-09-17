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

No Node runtime, npm packages or Go dependencies. Linux is the target; macOS
builds provide partial health data. `ss`, `ps`, `tmux`, `docker` and `find` supply
optional machine details. Their results are cached. The shared SSE sampler reads
Linux `/proc` once per second while connected and stops when no stream clients
remain. It reads process counters directly, with no `ps` in the one-second loop.
Browser connections stop in a hidden tab. The older state health sample slows
from 3 to 30 seconds after 15 seconds without a poll.

## Navigate the console

Use the collapsible left rail for Overview, Ports, Processes, Agents, Terminal,
Files, Docker, Boxes and Settings. Routes use hashes, such as `#/processes`, so
changing views keeps the page and stream alive. On phones, four direct links and
More form a five-item bottom bar. More opens the remaining views.

Each board has a filter with a clear action when nothing matches. Ports, Processes,
Agents and Docker also have sort controls. Terminal and Files fill the view; Files
has a path bar. Settings shows the running config, masks passwords and box tokens,
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
| `g a` | Agents |
| `g t` | Terminal |
| `g f` | Files |
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
Boxes uses the real `/api/boxes` response. It keeps the local box first and shows
an unreachable timestamp and a next step when another box does not respond.

## Install

Download the latest tagged release and run the interactive installer:

```sh
curl -fsSL https://raw.githubusercontent.com/wicolian/boxdeck/main/install.sh | sh
```

It chooses Linux or macOS and amd64 or arm64, installs to `~/.local/bin/boxdeck`,
and runs `boxdeck install`. On first install, choose a user, password and hostname.
On Linux it writes and starts `~/.config/systemd/user/boxdeck.service` with
`Nice=10`, then enables linger. Existing config is kept. On macOS, run `boxdeck serve`
after setup; automatic service installation uses Linux systemd.

The downloader needs a published `v*` release containing the Go binaries. To build
from a checkout with Go 1.27 or newer:

```sh
make build
./boxdeck version
./boxdeck install
```

Open `http://<your-box>:8100`. The default bind is loopback; use the automatic
Tailscale mirror or an SSH tunnel to reach it. Keep the deck on a private network.

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

## Herdr

Requires [herdr](https://github.com/herdrdev/herdr) 0.9 or newer. Boxdeck reads its
local socket at `~/.config/herdr/herdr.sock` while someone is watching, cached for
3 seconds. The card shows workspaces, tab counts and agent counts by status.
It disappears when herdr or its socket is absent.

Agent rows show the pane title, `working` / `idle` / `blocked` / `done` / `unknown`,
context and five-hour limit when supplied, and the pane ID. Click the row to focus
that pane in herdr on the box. The authenticated `/api/herdr/focus` endpoint sends
herdr's `agent.focus` socket request, equivalent to `herdr agent focus <pane_id>`.
Process entries are matched by their inherited herdr pane ID, not their folder name.

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
  "quick": [["App", 3001]],
  "reportRoots": ["~/reports", "~/box"],
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
