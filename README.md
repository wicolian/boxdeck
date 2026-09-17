# boxdeck

A one-page cockpit for your ssh dev box. Open it from your laptop, phone or iPad
and see, live:

- **Berths** – every port something is listening on, what it is, where it runs. Tap one and the app
  opens **inside the deck** (or in a new tab on a phone). Your `localhost:3001` on your iPad.
- **Health** – CPU, memory, swap, disk, load, with a sparkline history
- **Agents** – running `claude` / `codex` / `aider` / `opencode` / `goose` processes, their folder, tmux pane, model, age
- **tmux** sessions, **browsers** (headless Chrome count and memory), **docker** containers
- **Fresh reports** – the newest `.md` / `.html` / `.png` under the folders you name, linked through the file viewer

<!-- before-and-after:start -->
| Before | After |
|:---:|:---:|
| ![Before: ss, free, ps, tmux ls in a terminal](./captures/before.png) | ![After: boxdeck](./captures/after.png) |

| A dev server, viewed inside the deck |
|:---:|
| ![boxdeck viewer showing a localhost app](./captures/viewer.png) |

<details><summary>On a phone</summary>

| Preview (Phone) |
|:---:|
| <img src="./captures/after-phone.png" width="390" alt="boxdeck on a phone"> |

</details>
<!-- before-and-after:end -->

Zero dependencies. One Node file for the API, one HTML file for the page. It reads `/proc`
and shells out to `ss`, `ps`, `tmux`, `docker` and `find` only while someone is looking
(polls stop when the tab is hidden; sampling slows to every 30 s when nobody is watching).
Idle cost is a few MB of RAM and ~0 CPU.

Basic-auth protected. Binds to `127.0.0.1` by default: put it on your tailnet (or an ssh tunnel),
never on the public internet.

## Your localhost, from any device

Two things make `http://box:3001` work from your laptop, phone or iPad:

1. **Mirror.** boxdeck finds your Tailscale IP (`100.x.y.z`) and, for every port that is open on
   `127.0.0.1`, opens the same port on that IP and pipes bytes across. Plain TCP, so websockets,
   HMR and anything else just work. No sudo, no `tailscale serve`. On by default when a Tailscale
   interface exists (`"mirror": "auto"`); `true` / `false` to force; `"mirrorBind"` to use another
   private IP. Ports in `hide` are never mirrored. Mirrored ports carry no password of their own,
   same as `tailscale serve` — the tailnet is the wall.
2. **Viewer.** Click a berth and the app loads in a panel on the deck itself, with reload, pop-out
   and close. Apps that ask for a login (basic auth) cannot prompt inside a frame: open them once
   with ↗, sign in, come back.

## Install

```bash
git clone https://github.com/wicolian/boxdeck ~/boxdeck
cd ~/boxdeck && ./install.sh      # asks for a user, a password, and the hostname you use to reach the box
```

That writes `~/.config/boxdeck/config.json` and starts a user systemd service on port 8100.

With Tailscale on the box, the mirror publishes the deck itself on your tailnet IP too, so you are done.
Without it, expose port 8100 the way you like (`tailscale serve`, an ssh tunnel, a WireGuard peer),
never on the public internet.

Then bookmark `http://<your-box>:8100`. Your browser remembers the password.

## Config

`~/.config/boxdeck/config.json` (env vars `BOXDECK_*` override it):

```json
{
  "user": "me",
  "password": "…",
  "host": "box",
  "port": 8100,
  "title": "Deck",
  "filesPort": 8090,
  "quick": [["Files", 8090], ["Terminal", 7681], ["App", 3001]],
  "reportRoots": ["~/reports", "~/box"],
  "reportDays": 3,
  "mirror": "auto",
  "mirrorBind": "",
  "known": { "3001": "My app", "5173": "Vite demo" },
  "hide": [22, 53, 111, 139, 445],
  "agentPattern": "^(\\S*/)?(claude|codex|aider|opencode|goose)(\\s|$)"
}
```

- `host` is the name your browser uses to reach the box (`box`, `mybox.tailnet.ts.net`, `100.x.y.z`). Every port link is built from it.
- `quick` shortcuts sit in the masthead and dim when that port is closed.
- `known` labels ports; anything else gets the page `<title>` of whatever answers on it, or the process name.
- `filesPort` links each report through the file viewer below. Set `0` to list reports without links.

## Companions

**Files viewer** (`files-web.js`): serves your home folder, renders Markdown, lists folders,
same password. Run it on `filesPort`:

```bash
npm install marked            # optional, for Markdown rendering; without it .md shows as plain text
node files-web.js             # port from config.filesPort (default 8090)
```

**Terminal in the browser**: [ttyd](https://github.com/tsl0922/ttyd) with the same password, attached to tmux:

```bash
ttyd -p 7681 -i 127.0.0.1 -W -c me:PASSWORD tmux new-session -A -s web
```

Add both as user systemd services the same way `install.sh` does for boxdeck, expose the ports the
same way, and add them to `quick`. A tiny script that exposes every new local port automatically is in
`contrib/tailnet-autoserve`.

## Why

Agents write reports, start dev servers and open browsers on a box you are not sitting at.
Herdr, tmux and cmux show you the terminals; this shows you everything else, on one bookmark,
from anywhere on your tailnet.

MIT.
