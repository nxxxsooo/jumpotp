#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
version=$(tr -d '\r\n' < "$repo_dir/VERSION")
dist_dir="$repo_dir/dist"

mkdir -p "$dist_dir"

for target in darwin/arm64 darwin/amd64 linux/arm64 linux/amd64; do
  os=${target%/*}
  arch=${target#*/}
  case "$arch" in
    amd64) package_arch=x64 ;;
    *) package_arch=$arch ;;
  esac
  output="$dist_dir/jumpotp-$os-$package_arch"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build \
    -buildvcs=false \
    -trimpath \
    -ldflags "-s -w -X github.com/nxxxsooo/jumpotp/internal/version.Value=$version" \
    -o "$output" \
    "$repo_dir/cmd/jumpotp"
done
