# boxdeck

A one-page cockpit for your ssh dev box. Open it from your laptop, phone or iPad
and see, live:

- **Berths** – every port something is listening on, what it is, where it runs, one tap to open it
- **Health** – CPU, memory, swap, disk, load, with a sparkline history
- **Agents** – running `claude` / `codex` / `aider` / `opencode` / `goose` processes, their folder, tmux pane, model, age
- **tmux** sessions, **browsers** (headless Chrome count and memory), **docker** containers
- **Fresh reports** – the newest `.md` / `.html` / `.png` under the folders you name, linked through the file viewer

Zero dependencies. One Node file for the API, one HTML file for the page. It reads `/proc`
and shells out to `ss`, `ps`, `tmux`, `docker` and `find` only while someone is looking
(polls stop when the tab is hidden; sampling slows to every 30 s when nobody is watching).
Idle cost is a few MB of RAM and ~0 CPU.

Basic-auth protected. Binds to `127.0.0.1` by default: put it on your tailnet (or an ssh tunnel),
never on the public internet.

## Install

```bash
git clone https://github.com/wicolian/boxdeck ~/boxdeck
cd ~/boxdeck && ./install.sh      # asks for a user, a password, and the hostname you use to reach the box
```

That writes `~/.config/boxdeck/config.json` and starts a user systemd service on port 8100.

Expose it on your tailnet (plain TCP is fine, WireGuard already encrypts it):

```bash
sudo tailscale serve --bg --tcp=8100 tcp://127.0.0.1:8100
```

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
