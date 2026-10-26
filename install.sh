#!/bin/sh
# Install muretto from GitHub releases.
#
#   curl -fsSL https://raw.githubusercontent.com/tolgayayci/muretto/main/install.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/tolgayayci/muretto/main/install.sh | sh -s -- --version v1.2.3 --dir /usr/local/bin
#
# Options (or environment variables):
#   --version <vX.Y.Z>   MURETTO_VERSION      release to install (default: latest)
#   --dir <dir>          MURETTO_INSTALL_DIR  where to put the binary (default: ~/.local/bin)
#   --from-dir <dir>     (testing) read archives and checksums.txt from a local
#                        directory, e.g. GoReleaser's dist/, instead of GitHub
#
# The archive's sha256 is checked against the release's checksums.txt; any
# mismatch or missing entry aborts the install. Never uses sudo; never edits
# shell startup files.
#
# TODO(signing): verify a cosign signature of checksums.txt once releases are signed.
#
# Everything runs inside main(), called on the last line, so a truncated
# download executes nothing.

set -eu

REPO="tolgayayci/muretto"
BIN="muretto"

log() { printf '%s\n' "muretto-install: $*" >&2; }
die() { log "error: $*"; exit 1; }

usage() {
	cat >&2 <<'EOF'
usage: install.sh [--version vX.Y.Z] [--dir DIR] [--from-dir DIR]
  --version   release to install (default: latest; env MURETTO_VERSION)
  --dir       install directory (default: ~/.local/bin; env MURETTO_INSTALL_DIR)
  --from-dir  testing: use archives + checksums.txt from a local directory
EOF
}

has() { command -v "$1" >/dev/null 2>&1; }

detect_os() {
	case "$(uname -s)" in
		Darwin) echo darwin ;;
		Linux) echo linux ;;
		*) die "unsupported OS $(uname -s); prebuilt binaries exist for macOS and Linux (on Windows, use WSL)" ;;
	esac
}

detect_arch() {
	_os=$1
	case "$(uname -m)" in
		x86_64 | amd64) _arch=amd64 ;;
		arm64 | aarch64) _arch=arm64 ;;
		*) die "unsupported CPU $(uname -m); prebuilt binaries exist for amd64 and arm64" ;;
	esac
	# A shell running under Rosetta reports x86_64 on Apple silicon; prefer native.
	if [ "$_os" = darwin ] && [ "$_arch" = amd64 ] &&
		[ "$(sysctl -n hw.optional.arm64 2>/dev/null || echo 0)" = 1 ]; then
		_arch=arm64
	fi
	echo "$_arch"
}

# download <url> <output file>; https only.
download() {
	if has curl; then
		curl --proto '=https' --tlsv1.2 -fsSL -o "$2" "$1"
	elif has wget; then
		wget --https-only -q -O "$2" "$1"
	else
		die "need curl or wget"
	fi
}

sha256_of() {
	if has sha256sum; then
		sha256sum "$1" | awk '{print $1}'
	elif has shasum; then
		shasum -a 256 "$1" | awk '{print $1}'
	elif has openssl; then
		openssl dgst -sha256 "$1" | awk '{print $NF}'
	else
		die "need sha256sum, shasum or openssl to verify the download"
	fi
}

latest_version() {
	_json="$1/latest.json"
	download "https://api.github.com/repos/$REPO/releases/latest" "$_json" ||
		die "could not look up the latest release (set --version or MURETTO_VERSION)"
	sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$_json" | head -n 1
}

# Version from checksums.txt in a local directory (testing with --from-dir).
local_version() {
	sed -n "s/^[0-9a-f]\{64\}[[:space:]][[:space:]]*[*]\{0,1\}${BIN}_\(.*\)_$2_$3\.tar\.gz\$/\1/p" "$1/checksums.txt" | head -n 1
}

main() {
	version=${MURETTO_VERSION:-}
	install_dir=${MURETTO_INSTALL_DIR:-}
	from_dir=""

	while [ $# -gt 0 ]; do
		case "$1" in
			--version) [ $# -ge 2 ] || die "--version needs a value"; version=$2; shift 2 ;;
			--version=*) version=${1#*=}; shift ;;
			--dir) [ $# -ge 2 ] || die "--dir needs a value"; install_dir=$2; shift 2 ;;
			--dir=*) install_dir=${1#*=}; shift ;;
			--from-dir) [ $# -ge 2 ] || die "--from-dir needs a value"; from_dir=$2; shift 2 ;;
			--from-dir=*) from_dir=${1#*=}; shift ;;
			-h | --help) usage; exit 0 ;;
			*) die "unknown option $1 (see --help)" ;;
		esac
	done

	if [ -z "$install_dir" ]; then
		[ -n "${HOME:-}" ] || die "HOME is not set; pass --dir"
		install_dir="$HOME/.local/bin"
	fi

	os=$(detect_os)
	arch=$(detect_arch "$os")

	tmp=$(mktemp -d 2>/dev/null || mktemp -d -t muretto)
	trap 'rm -rf "$tmp"' EXIT
	trap 'exit 130' INT
	trap 'exit 143' TERM

	if [ -n "$from_dir" ]; then
		[ -f "$from_dir/checksums.txt" ] || die "$from_dir/checksums.txt not found"
		[ -n "$version" ] || version=$(local_version "$from_dir" "$os" "$arch")
	elif [ -z "$version" ]; then
		version=$(latest_version "$tmp")
	fi
	[ -n "$version" ] || die "could not determine the version to install"
	version=${version#v}
	case "$version" in
		*[!0-9A-Za-z.+-]*) die "invalid version '$version'" ;;
	esac

	archive="${BIN}_${version}_${os}_${arch}.tar.gz"
	if [ -n "$from_dir" ]; then
		log "installing $BIN $version ($os/$arch) from $from_dir"
		cp "$from_dir/checksums.txt" "$tmp/checksums.txt"
		[ -f "$from_dir/$archive" ] || die "$from_dir/$archive not found"
		cp "$from_dir/$archive" "$tmp/$archive"
	else
		base="https://github.com/$REPO/releases/download/v$version"
		log "installing $BIN $version ($os/$arch) from $base"
		download "$base/checksums.txt" "$tmp/checksums.txt" || die "could not download checksums.txt for v$version"
		download "$base/$archive" "$tmp/$archive" || die "could not download $archive"
	fi

	expected=$(awk -v f="$archive" '$2 == f || $2 == "*" f { print $1; exit }' "$tmp/checksums.txt")
	[ -n "$expected" ] || die "$archive is not listed in checksums.txt; refusing to install"
	actual=$(sha256_of "$tmp/$archive")
	if [ "$expected" != "$actual" ]; then
		die "checksum mismatch for $archive (expected $expected, got $actual); refusing to install"
	fi
	log "sha256 verified: $actual"

	tar -xzf "$tmp/$archive" -C "$tmp" "$BIN" || die "could not extract $BIN from $archive"
	[ -f "$tmp/$BIN" ] || die "$archive does not contain $BIN"

	mkdir -p "$install_dir" 2>/dev/null || die "cannot create $install_dir (choose another with --dir)"
	[ -w "$install_dir" ] || die "$install_dir is not writable (choose another with --dir; this script never uses sudo)"
	# Copy next to the target, then rename: atomic, and safe if muretto is running.
	staged="$install_dir/.$BIN.tmp.$$"
	cp "$tmp/$BIN" "$staged"
	chmod 0755 "$staged"
	mv -f "$staged" "$install_dir/$BIN"

	log "installed $install_dir/$BIN"
	"$install_dir/$BIN" version >&2 || true

	case ":${PATH:-}:" in
		*":$install_dir:"*) ;;
		*) log "note: $install_dir is not in your PATH. Add this to your shell profile:"
		   log "  export PATH=\"$install_dir:\$PATH\"" ;;
	esac
	log "next: run '$BIN init' to connect Claude Code"
}

main "$@"
