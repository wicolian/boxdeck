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
curl -sS -H "Authorization: Bearer $TOKEN" "$BOXDECK_TO/api/usage?days=7"
curl -sS -H "Authorization: Bearer $TOKEN" "$BOXDECK_TO/api/usage/all"
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
`url`, `health`, `agents`, `ports`, and a `usage` object with today's provider
tokens, cost, and quota.

`usage` reads the Claude and Codex JSONL session ledgers already stored on the
device. It returns up to 30 UTC daily rows, a local-day label, per-model token
breakdowns, session counts, and cost estimates labeled `estimate at list price`.
Claude quota comes from the OAuth usage endpoint when `~/.claude/.credentials.json`
is present. Codex quota comes from recent session rate limits when available;
null rate limits are treated as an API-key account. `usage.json` is private,
written atomically under the local data directory, and never contains prompts,
credentials, or access tokens.

```json
{
  "providers": {
    "claude": {
      "quota": {"fiveHour": {"pct": 20, "resetsAt": "2026-09-17T23:10:00.471440+00:00"}, "sevenDay": {"pct": 68, "resetsAt": "2026-09-19T18:59:59.471461+00:00"}, "models": null, "sampledAt": "2026-09-17T22:39:52Z"},
      "today": {"tokens": {"in": 3062, "cachedIn": 508593677, "cacheWrite": 5809401, "out": 1247876}, "costUsd": 0},
      "localDay": "2026-09-17",
      "days": [{"day": "2026-09-17", "tokens": {"in": 3062, "cachedIn": 508593677, "cacheWrite": 5809401, "out": 1247876}, "costUsd": 0, "models": {"claude-opus-5": {"tokens": {"in": 2942, "cachedIn": 500597247, "cacheWrite": 5631266, "out": 1218313}, "costUsd": 0, "priceSet": false}, "claude-sonnet-5": {"tokens": {"in": 120, "cachedIn": 7996430, "cacheWrite": 178135, "out": 29563}, "costUsd": 0, "priceSet": false}}}],
      "sessions": 124
    },
    "codex": {
      "quota": null,
      "today": {"tokens": {"in": 726500094, "cachedIn": 718548248, "cacheWrite": 7938929, "out": 1288237}, "costUsd": 636.2414239300001},
      "localDay": "2026-09-17",
      "days": [{"day": "2026-09-17", "tokens": {"in": 726500094, "cachedIn": 718548248, "cacheWrite": 7938929, "out": 1288237}, "costUsd": 636.2414239300001, "models": {"gpt-5.6-luna": {"tokens": {"in": 684986724, "cachedIn": 678294694, "cacheWrite": 6683693, "out": 1054965}, "costUsd": 153.50011993000004, "priceSet": true}, "gpt-6-astra": {"tokens": {"in": 41513370, "cachedIn": 40253554, "cacheWrite": 1255236, "out": 233272}, "costUsd": 482.74130399999996, "priceSet": true}}}],
      "sessions": 115
    },
  },
  "device": "box",
  "updatedAt": "2026-09-17T22:39:52Z"
}
```

The JSON above is a trimmed real response from the isolated port 8103 QA run on
2026-09-17. It shows provider totals and the latest daily row while omitting
older daily rows.

`usage/all` returns the same provider shape with sums from the local box and
reachable configured boxes, plus a `boxes` array containing each box's
provider breakdown. Unreachable boxes remain in that array with `ok:false` and
are not included in sums.

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
boxdeck ctl --box old-thinkpad usage --table
```

The standalone command does not need a running deck:

```sh
boxdeck usage --days 7 --table
boxdeck usage --days 30 --json
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
