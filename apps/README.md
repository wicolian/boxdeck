# App recipes

Apps are JSON recipes. Built-ins live in this folder. A user can add recipes
under `~/.config/boxdeck/apps` or in the top-level `apps` array in the
boxdeck config. The load order is embedded built-ins, config recipes, then
home recipes. A later recipe with the same `id` replaces an earlier one.

The smallest useful recipe is:

```json
{
  "id": "my-tool",
  "name": "My tool",
  "tag": "developer tool",
  "detect": {"bin": "my-tool"},
  "install": {"hint": "Install my-tool", "url": "https://example.com"},
  "start": {"cmd": ["my-tool", "--port", "{port}"], "port": 0},
  "stop": "signal",
  "open": {"url": "http://{host}:{port}", "embed": true},
  "health": {"http": "http://127.0.0.1:{port}/"},
  "docs": "https://example.com/docs"
}
```

Detection accepts a `bin` on PATH, a listening `port`, or an existing `file`.
These conditions are alternatives. A start port of `0` chooses a free local
port. Templates expand `{port}`, `{host}`, `{home}`, and `{id}`. `open.path`
can point at a deck route such as `#/browser`. Use `stop: "signal"` for a
managed process group, or provide a command array for a custom stop command.

Health checks use a two second timeout. The Apps view refreshes health while it
is open. Managed stdout and stderr append to
`~/.local/share/boxdeck/apps/<id>.log`; the view shows the last 20 lines and
the log endpoint accepts up to 1000 lines.

To contribute a recipe, add one JSON file, test it on a box where the tool is
installed, and open a PR. Keep commands as argument arrays. Do not put tokens,
passwords, or customer data in a recipe.
