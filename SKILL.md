# Boxdeck agent skill

Use this when an agent needs exact machine facts or a controlled action on a
remote box. It is useful for ports, processes, agent panes, tmux, files, and
commands where a screenshot would lose precision.

Install the single binary:

```sh
curl -fsSL https://raw.githubusercontent.com/wicolian/boxdeck/main/install.sh | sh
```

Set the target and a token without putting either in source or reports:

```sh
export BOXDECK_TO=http://box:8100
export BOXDECK_TOKEN="$TOKEN"
```

For a shared tailnet fleet, install the same token on every device and include
it in each device's `tokens` list:

```sh
boxdeck install --fleet-token "$FLEET_TOKEN"
```

The Network view discovers online peers and the Usage and Boxes views include
their usage when the fleet token is accepted.

Commands and example output:

```text
boxdeck ctl state
title	Deck
host	127.0.0.1

boxdeck ctl --table ports
PORT	PROC	PID	URL
7681	ttyd	314988	http://127.0.0.1:7681
8103	boxdeck-phase2-	338699	http://127.0.0.1:8103

boxdeck ctl --table procs
PID	CPU	RSS	ARGS
338018	0.2	12673024	/tmp/boxdeck-phase2-api serve

boxdeck ctl --table agents
PID	KIND	STATUS	PANE	CWD
318017	codex	working	w8:p8S	~/codes/boxdeck/.worktrees/phase2-api

boxdeck ctl --table tmux
map[attached:true name:web windows:2]

boxdeck ctl files reports/latest.md
# Latest report

boxdeck ctl focus w1:p1
focused	w1:p1

boxdeck ctl prompt w1:p1 "check the failing test"
ok	true

boxdeck ctl kill 338018
killed	338018

boxdeck ctl run "printf ready"
boxdeck ctl browser start
boxdeck ctl browser pages
boxdeck ctl browser shot PAGE screenshot.png
boxdeck ctl browser stop
ready
exit 0

boxdeck ctl --table usage
PROVIDER	TODAY TOKENS	TODAY COST	SESSIONS
claude	0	$0.0000	0
codex	0	$0.0000	0
estimate at list price
```

Apps can be managed through the local deck or a bearer-token client:

```sh
boxdeck app list
boxdeck app start t3code
boxdeck app stop t3code
boxdeck app restart t3code
boxdeck app log t3code
boxdeck ctl apps
boxdeck ctl --table apps
boxdeck ctl app start t3code
boxdeck ctl app stop t3code
boxdeck ctl app restart t3code
boxdeck ctl app log t3code
```

An app is a JSON recipe. Built-in recipes are embedded in `apps/`. Config
recipes in the `apps` array override built-ins, and files in
`~/.config/boxdeck/apps` override both. Detection can use a binary, a port, or
a file. See [apps/README.md](apps/README.md) for the full shape and how to
contribute a recipe.

Usage is also available without a running deck. `boxdeck usage --days 7
--table` reads local Claude and Codex ledgers directly, while `--json` prints
the same provider response used by the Usage view. The deck's `/api/usage`
endpoint accepts `days=1` through `days=30`; `/api/usage/all` adds the local
box and configured remote boxes. Costs are estimates at list price, and a
config entry such as `"pricing":{"model":{"gpt-6-astra":{"in":10,
"cachedIn":1,"cacheWrite":12.5,"out":50}}}` overrides a model's rates.

Use `boxdeck ctl --box NAME ...` for a machine in the local `boxes` config.
The remote box token stays server-side when the Boxes view fetches health.
Mirrored ports carry no password. The private tailnet is the wall, so only
mirror a port to a trusted tailnet and keep the deck itself authenticated.

`allowRun` is off by default. Enabling it gives a bearer-token caller shell
execution as the boxdeck service account, so leave it off unless that authority
is intended.

## Remote browser control

Boxdeck starts one managed headless Chromium or Chrome per box and serves its
CDP endpoint through the authenticated deck. From another device on the private
network, use the token query accepted by CDP clients:

```sh
AGENT_BROWSER_IDLE_TIMEOUT_MS=0 agent-browser --cdp \
  "http://box:8100/cdp?token=$TOKEN" open https://example.com
AGENT_BROWSER_IDLE_TIMEOUT_MS=0 agent-browser --cdp \
  "http://box:8100/cdp?token=$TOKEN" screenshot
```

The Browser view shows pages and captures a page screenshot. Stop the managed
browser when the task is complete. `/cdp/` and mirrored ports are private
network surfaces. Mirrored ports carry no password.
