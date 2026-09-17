# Boxdeck API

The API is on the same origin as the deck. All endpoints except `/api/health`
need either the deck cookie, Basic authentication, or a bearer token. Create a
token with `boxdeck token new`, keep it in an environment variable, and send it
as `Authorization: Bearer $TOKEN`.

The additive config fields are:

```json
{
  "tokens": [],
  "boxes": [{"name":"old-thinkpad","url":"http://thinkpad:8100","token":"..."}],
  "allowRun": false
}
```

`boxdeck token list` shows only token prefixes. `boxdeck token revoke PREFIX`
removes one matching token. A token is printed in full only by `token new`.

## Read endpoints

```sh
curl -sS -H "Authorization: Bearer $TOKEN" "$BOXDECK_TO/api/state"
curl -sS -H "Authorization: Bearer $TOKEN" "$BOXDECK_TO/api/ports"
curl -sS -H "Authorization: Bearer $TOKEN" "$BOXDECK_TO/api/procs?sort=cpu&n=20"
curl -sS -H "Authorization: Bearer $TOKEN" "$BOXDECK_TO/api/agents"
curl -sS -H "Authorization: Bearer $TOKEN" "$BOXDECK_TO/api/tmux"
curl -sS -H "Authorization: Bearer $TOKEN" "$BOXDECK_TO/api/docker"
curl -sS -H "Authorization: Bearer $TOKEN" "$BOXDECK_TO/api/boxes"
curl -sS -H "Authorization: Bearer $TOKEN" "$BOXDECK_TO/api/files?path=reports"
curl -sS "$BOXDECK_TO/api/health"
```

`state` is the deck payload: health, history, ports, agents, tmux, browsers,
Docker, reports, mirror status, terminal status, and the configured shortcuts.
`ports` includes exact port numbers, process, PID, working directory, title, and
URL. `procs` accepts `sort=cpu`, `sort=mem`, `sort=age`, or `sort=pid`, and `n`
from 1 to 200. The default is the top 20 by CPU.

`agents` is the process view merged with tmux and herdr. `tmux` is a compact
session list. `docker` is the cached Docker process list.

`files` returns JSON for a directory:

```json
{"path":"/reports","entries":[{"name":"latest.md","dir":false,"size":1234,"modTime":"2026-09-17T21:00:00Z"}]}
```

For a regular file it returns the file bytes with a matching content type.
Paths are lexical descendants of `filesRoot`; symlinks are allowed in the same
way as the deck file viewer. Hidden entries and `node_modules` are omitted from
directory listings.

`health` is unauthenticated and returns only `{"ok":true,"version":"dev"}`.
It is suitable for a load balancer probe.

`boxes` is fetched server-side. The local box is first. Each configured remote
box is queried in parallel with its token kept on the server, a 3 second
per-box timeout, and a 5 second cache. A failed box has `ok:false` and `since`
set to the RFC3339 time of its first failure. Its card also includes `name`,
`url`, `health`, `agents`, and `ports`.

## Actions

```sh
curl -sS -X POST -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"pid":1234,"signal":15}' \
  "$BOXDECK_TO/api/proc/kill"
curl -sS -X POST -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"pane_id":"w1:p1"}' \
  "$BOXDECK_TO/api/herdr/focus"
curl -sS -X POST -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"pane_id":"w1:p1","text":"check the failing test"}' \
  "$BOXDECK_TO/api/herdr/prompt"
curl -sS -X POST -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"cmd":"go test ./...","cwd":"~/src/app","timeoutMs":60000}' \
  "$BOXDECK_TO/api/run"
```

`proc/kill` accepts a PID and signal. The live stream and process guard are
implemented by the UI worker against this contract.

`herdr/focus` sends herdr's `agent.focus` request. `herdr/prompt` validates the
pane and runs `herdr agent prompt PANE TEXT` without a shell.

`run` is disabled unless the config contains `"allowRun": true`. When enabled,
it gives an authenticated caller arbitrary command execution as the boxdeck
service user through `bash -lc`, with an optional working directory, captured
stdout and stderr, an exit code, and a hard 60 second timeout. This opens the
same authority as the service account, including access to its files, sockets,
credentials, and network. Keep it false unless the bearer token is restricted
to a trusted private network.

The live endpoint is `GET /api/stream`. It is an authenticated SSE stream with
one event per second while connected: per-core and total CPU, memory, swap,
load, network receive and transmit bytes per second, disk read and write bytes
per second, and the top eight processes by CPU and memory. `POST /api/proc/kill`
is the process-page action endpoint.

## Remote client

The same binary includes `ctl`. JSON is the default output; add `--table` for a
compact human view. `--to` and `--token` can be replaced by `BOXDECK_TO` and
`BOXDECK_TOKEN`. `--box NAME` selects a configured box and keeps its token in
the local process.

```sh
boxdeck ctl --to "$BOXDECK_TO" --token "$TOKEN" state
boxdeck ctl --to "$BOXDECK_TO" --token "$TOKEN" --table ports
boxdeck ctl --box old-thinkpad agents
boxdeck ctl --box old-thinkpad focus w1:p1
boxdeck ctl --box old-thinkpad prompt w1:p1 "check the failing test"
boxdeck ctl --box old-thinkpad kill 1234
boxdeck ctl --box old-thinkpad files reports/latest.md
boxdeck ctl --box old-thinkpad run "go test ./..."
```

The 8103 QA run produced this real table output (the process and port rows are
machine state at the time of the check):

```text
$ boxdeck ctl --box local --table state
title	Deck
host	127.0.0.1

$ boxdeck ctl --box local --table ports
PORT	PROC	PID	URL
7681	ttyd	314988	http://127.0.0.1:7681
8090	boxdeck	314979	http://127.0.0.1:8090
8100	boxdeck	314979	http://127.0.0.1:8100
8103	boxdeck-phase2-	338699	http://127.0.0.1:8103

$ boxdeck ctl --box local --table run "printf ready"
ready
exit 0
```
