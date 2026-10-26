#!/usr/bin/env node
// Fill the npm platform packages from GoReleaser archives and set versions.
//
//   node npm/scripts/assemble.mjs <version> [--dist <dir>]
//
// <version> is the release version without the "v" (e.g. 1.2.3, as in
// muretto_1.2.3_darwin_arm64.tar.gz). <dir> holds the archives and
// checksums.txt (default: npm/.dist, where the release workflow downloads them;
// for a local snapshot pass --dist dist).
//
// For each platform it checks the archive's sha256 against checksums.txt
// (refusing on a mismatch or a missing line), extracts only the `muretto`
// binary into npm/<pkg>/bin/, copies LICENSE, and writes <version> into all
// five package.json files and the launcher's optionalDependencies.
// It never publishes anything.
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import {
  chmodSync, copyFileSync, existsSync, mkdirSync, mkdtempSync,
  readFileSync, rmSync, writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const NPM_DIR = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const REPO_DIR = resolve(NPM_DIR, "..");

// npm package -> GoReleaser {{ .Os }}_{{ .Arch }}
const PLATFORMS = {
  "muretto-darwin-arm64": "darwin_arm64",
  "muretto-darwin-x64": "darwin_amd64",
  "muretto-linux-arm64": "linux_arm64",
  "muretto-linux-x64": "linux_amd64",
};

// Semver 2.0 (no leading "v").
const SEMVER =
  /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/;

function die(msg) {
  console.error(`assemble: ${msg}`);
  process.exit(1);
}

function parseArgs(argv) {
  let version;
  let dist = join(NPM_DIR, ".dist");
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === "--dist") {
      if (!argv[i + 1]) die("--dist needs a directory");
      dist = resolve(argv[++i]);
    } else if (a.startsWith("--dist=")) {
      dist = resolve(a.slice("--dist=".length));
    } else if (a.startsWith("-")) {
      die(`unknown flag ${a}`);
    } else if (version === undefined) {
      version = a;
    } else {
      die(`unexpected argument ${a}`);
    }
  }
  if (!version) die("usage: node npm/scripts/assemble.mjs <version> [--dist <dir>]");
  if (version.startsWith("v")) version = version.slice(1);
  if (!SEMVER.test(version)) die(`"${version}" is not a semver version`);
  return { version, dist };
}

function readChecksums(dist) {
  const file = join(dist, "checksums.txt");
  if (!existsSync(file)) die(`${file} not found`);
  const sums = new Map();
  for (const line of readFileSync(file, "utf8").split("\n")) {
    const m = line.match(/^([0-9a-f]{64}) [ *]?(\S+)$/);
    if (m) sums.set(m[2], m[1]);
  }
  return sums;
}

function sha256(file) {
  return createHash("sha256").update(readFileSync(file)).digest("hex");
}

function readJSON(file) {
  return JSON.parse(readFileSync(file, "utf8"));
}

function writeJSON(file, value) {
  writeFileSync(file, JSON.stringify(value, null, 2) + "\n");
}

function checkNoScripts(pkg, file) {
  // Install-time scripts are a supply-chain risk and are not needed here.
  if (pkg.scripts && Object.keys(pkg.scripts).length > 0) {
    die(`${file} must not define "scripts"`);
  }
}

function main() {
  const { version, dist } = parseArgs(process.argv.slice(2));
  const sums = readChecksums(dist);
  const license = join(REPO_DIR, "LICENSE");

  for (const [pkgName, target] of Object.entries(PLATFORMS)) {
    const archive = `muretto_${version}_${target}.tar.gz`;
    const archivePath = join(dist, archive);
    if (!existsSync(archivePath)) die(`${archivePath} not found`);
    const want = sums.get(archive);
    if (!want) die(`no checksum for ${archive} in checksums.txt`);
    const got = sha256(archivePath);
    if (got !== want) die(`checksum mismatch for ${archive}: want ${want}, got ${got}`);

    const pkgDir = join(NPM_DIR, pkgName);
    const binDir = join(pkgDir, "bin");
    const tmp = mkdtempSync(join(tmpdir(), "muretto-assemble-"));
    try {
      // Extract only the binary; nothing else from the archive is trusted.
      execFileSync("tar", ["-xzf", archivePath, "-C", tmp, "muretto"], { stdio: "inherit" });
      rmSync(binDir, { recursive: true, force: true });
      mkdirSync(binDir, { recursive: true });
      copyFileSync(join(tmp, "muretto"), join(binDir, "muretto"));
      chmodSync(join(binDir, "muretto"), 0o755);
    } finally {
      rmSync(tmp, { recursive: true, force: true });
    }
    if (existsSync(license)) copyFileSync(license, join(pkgDir, "LICENSE"));

    const pkgFile = join(pkgDir, "package.json");
    const pkg = readJSON(pkgFile);
    if (pkg.name !== pkgName) die(`${pkgFile} has name ${pkg.name}, want ${pkgName}`);
    checkNoScripts(pkg, pkgFile);
    pkg.version = version;
    writeJSON(pkgFile, pkg);
    console.log(`assemble: ${pkgName}@${version} <- ${archive} (sha256 ${got.slice(0, 12)}…)`);
  }

  const launcherDir = join(NPM_DIR, "muretto");
  const launcherFile = join(launcherDir, "package.json");
  const launcher = readJSON(launcherFile);
  checkNoScripts(launcher, launcherFile);
  launcher.version = version;
  launcher.optionalDependencies = Object.fromEntries(
    Object.keys(PLATFORMS).map((name) => [name, version]),
  );
  writeJSON(launcherFile, launcher);
  if (existsSync(license)) copyFileSync(license, join(launcherDir, "LICENSE"));
  console.log(`assemble: muretto@${version} (launcher) with ${Object.keys(PLATFORMS).length} optionalDependencies`);
}

main();
