#!/usr/bin/env node
// Launcher for the prebuilt muretto Go binary.
//
// The binary ships in one optional dependency per platform (muretto-<os>-<cpu>);
// npm installs only the one whose "os"/"cpu" match. This file finds it and runs
// it with the same arguments, stdio, exit code and signals. No postinstall, no
// network access.
//
// The child gets MURETTO_LAUNCHER_BINARY=<absolute path of the real binary>.
// `muretto init` must write that real path (os.Executable()) into Claude Code
// hooks, never `npx muretto`. See docs/INSTALL.md.
"use strict";

const { spawn } = require("node:child_process");
const path = require("node:path");
const os = require("node:os");

const PLATFORMS = {
  "darwin-arm64": "muretto-darwin-arm64",
  "darwin-x64": "muretto-darwin-x64",
  "linux-arm64": "muretto-linux-arm64",
  "linux-x64": "muretto-linux-x64",
};

const OTHER_INSTALLS =
  "Other ways to install: `brew install tolgayayci/tap/muretto`, or\n" +
  "  curl -fsSL https://raw.githubusercontent.com/tolgayayci/muretto/main/install.sh | sh\n" +
  "See https://github.com/tolgayayci/muretto/blob/main/docs/INSTALL.md";

function fail(msg) {
  process.stderr.write(`muretto: ${msg}\n`);
  process.exit(1);
}

function binaryPath() {
  const key = `${process.platform}-${process.arch}`;
  const pkg = PLATFORMS[key];
  if (!pkg) {
    fail(
      `unsupported platform "${key}".\n` +
        `Prebuilt binaries exist for: ${Object.keys(PLATFORMS).join(", ")}.\n` +
        (process.platform === "win32" ? "On Windows, use WSL.\n" : "") +
        "You can also build from source: go install github.com/tolgayayci/muretto/cmd/muretto@latest",
    );
  }
  let dir;
  try {
    dir = path.dirname(require.resolve(`${pkg}/package.json`));
  } catch {
    fail(
      `the platform package "${pkg}" is not installed.\n` +
        "npm installs it as an optional dependency; it was probably skipped by\n" +
        "--omit=optional, --no-optional, or a lockfile made on another platform.\n" +
        "Reinstall without that flag (or delete the lockfile and node_modules).\n" +
        OTHER_INSTALLS,
    );
  }
  return path.join(dir, "bin", "muretto");
}

function run() {
  const bin = binaryPath();
  const child = spawn(bin, process.argv.slice(2), {
    stdio: "inherit",
    env: { ...process.env, MURETTO_LAUNCHER_BINARY: bin },
  });

  // Forward termination signals so `kill <node pid>` reaches the binary too.
  const signals = ["SIGINT", "SIGTERM", "SIGHUP"];
  const forward = (sig) => {
    if (child.exitCode === null && child.signalCode === null) child.kill(sig);
  };
  for (const sig of signals) process.on(sig, forward);

  child.on("error", (err) => {
    fail(
      `cannot run ${bin}: ${err.message}\n` +
        (err.code === "EACCES" ? "The file is not executable; reinstall the package.\n" : "") +
        OTHER_INSTALLS,
    );
  });

  child.on("exit", (code, signal) => {
    for (const sig of signals) process.removeListener(sig, forward);
    if (signal) {
      // Die from the same signal so the parent shell sees it.
      process.kill(process.pid, signal);
      // Fallback if the signal is ignored here.
      const n = os.constants.signals[signal] || 0;
      setTimeout(() => process.exit(128 + n), 200);
      return;
    }
    process.exit(code === null ? 1 : code);
  });
}

run();
