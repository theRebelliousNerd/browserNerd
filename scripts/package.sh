#!/bin/bash
set -e

PLATFORM=$1
ARCH=$2

if [ -z "$PLATFORM" ] || [ -z "$ARCH" ]; then
  echo "Usage: npm run package -- <platform> <arch>"
  exit 1
fi

NAME="browsernerd"
OUT_DIR="release_build/${PLATFORM}.${ARCH}.${NAME}"

echo "Packaging for ${PLATFORM} ${ARCH}..."

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"

cp gemini-extension.json "$OUT_DIR/"
cp GEMINI.md "$OUT_DIR/"
cp package.json "$OUT_DIR/"
cp -r commands/ "$OUT_DIR/"
cp -r hooks/ "$OUT_DIR/"
cp -r skills/ "$OUT_DIR/"
cp -r scripts/ "$OUT_DIR/"
cp -r mcp-server/gemini-config.yaml "$OUT_DIR/"
mkdir -p "$OUT_DIR/bin"
cp bin/cli.js "$OUT_DIR/bin/"

GOOS=$PLATFORM
if [ "$PLATFORM" = "win32" ]; then GOOS="windows"; fi
if [ "$PLATFORM" = "darwin" ]; then GOOS="darwin"; fi

GOARCH="amd64"
if [ "$ARCH" = "arm64" ]; then GOARCH="arm64"; fi

BIN_SRC="bin/browsernerd-${GOOS}-${GOARCH}"
BIN_DST="$OUT_DIR/bin/browsernerd-${PLATFORM}-${ARCH}"
if [ "$PLATFORM" = "win32" ]; then
  BIN_SRC="${BIN_SRC}.exe"
  BIN_DST="${BIN_DST}.exe"
fi

if [ -f "$BIN_SRC" ]; then
  cp "$BIN_SRC" "$BIN_DST"
else
  echo "WARNING: Prebuilt binary $BIN_SRC not found."
fi

mkdir -p release
cd "release_build/${PLATFORM}.${ARCH}.${NAME}"

if [ "$PLATFORM" = "win32" ]; then
  zip -r "../../release/${PLATFORM}.${ARCH}.${NAME}.zip" ./*
else
  tar -czvf "../../release/${PLATFORM}.${ARCH}.${NAME}.tar.gz" ./*
fi

echo "Created release archive successfully"
