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
ready
exit 0

boxdeck ctl --table usage
PROVIDER	TODAY TOKENS	TODAY COST	SESSIONS
claude	0	$0.0000	0
codex	0	$0.0000	0
estimate at list price
```

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
