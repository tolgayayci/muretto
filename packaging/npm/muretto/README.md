# muretto

See what your AI coding agents created outside the repo, which session created it, and whether it is open to anyone. Read-only.

```sh
npm install -g muretto
muretto init
```

`npx muretto` works for a quick look. Install globally before running `muretto init`: the Claude Code hooks point at the real binary, and an `npx` cache can be cleaned at any time.

This package is a small Node launcher. npm installs the Go binary from one platform package (`muretto-darwin-arm64`, `muretto-darwin-x64`, `muretto-linux-arm64` or `muretto-linux-x64`) as an optional dependency. There is no install script and nothing is downloaded from outside the npm registry.

Docs and other install methods: <https://github.com/tolgayayci/muretto>
