# muretto

A read-only "git log for infrastructure" for Claude Code: see what your AI coding agents created outside the repo, who asked for it, and whether it is exposed.

```sh
npx muretto version
npm install -g muretto   # recommended if you will run `muretto init`
```

This package is a small Node launcher. The Go binary comes from one platform package (`muretto-darwin-arm64`, `muretto-darwin-x64`, `muretto-linux-arm64` or `muretto-linux-x64`), installed by npm as an optional dependency. There is no postinstall script and no download from outside the npm registry.

**Hooks:** `muretto init` points Claude Code hooks at the real binary inside the platform package, not at `npx`. An `npx` cache can be cleaned at any time, so install globally (or use Homebrew or `install.sh`) before running `muretto init`.

Other install methods and details: <https://github.com/tolgayayci/muretto/blob/main/docs/INSTALL.md>
