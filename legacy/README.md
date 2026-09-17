# Frozen Node version

These files are frozen for one release. Use the Go binary from the repository root
for new installs. The old deck and files viewer still run with Node 18 or newer:

```sh
node legacy/server.js
node legacy/files-web.js
```

The copied page and installer preserve the old separate-port behavior.
