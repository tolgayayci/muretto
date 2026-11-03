#!/bin/sh
# Build the five npm packages on this machine, without publishing anything.
#
#   packaging/npm/scripts/pack-local.sh <version>
#
# Only for the first publish: npm trusted publishing can only be set up on a
# package that already exists. After that, the release workflow publishes from
# a v* tag.
#
# Builds the archives with GoReleaser from HEAD (which must be clean), checks
# and assembles the packages with assemble.mjs, packs them into
# packaging/npm/.pack/ and restores the package.json files it rewrote.
set -eu

version=${1:?usage: pack-local.sh <version>}
version=${version#v}
root=$(cd "$(dirname "$0")/../../.." && pwd)
cd "$root"

if [ -n "$(git status --porcelain)" ]; then
  echo "pack-local: the working tree is not clean; commit or stash first" >&2
  exit 1
fi

goreleaser=${GORELEASER:-go run github.com/goreleaser/goreleaser/v2@v2.18.3}
GORELEASER_CURRENT_TAG="v$version" $goreleaser release --clean --skip=publish,announce,validate

node packaging/npm/scripts/assemble.mjs "$version" --dist dist

pack=packaging/npm/.pack
rm -rf "$pack"
mkdir -p "$pack"
for dir in darwin-arm64 darwin-x64 linux-arm64 linux-x64 muretto; do
  npm pack "./packaging/npm/$dir" --pack-destination "$pack" --ignore-scripts
done
git checkout -- packaging/npm

echo
echo "pack-local: built from $(git rev-parse --short HEAD); publish the platform packages first:"
ls -1 "$pack"
