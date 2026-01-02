#!/usr/bin/env bash
# Build winres-gen on Linux/macOS.
#
# Targets:
#   build  (default)  compile ./winres-gen for the host platform
#   clean             remove build output
#   test              gofmt check, go vet, go test
#   all               clean, test, then build
#   cross             build the full release matrix into dist/
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
out="${root}/winres-gen"
dist="${root}/dist"

if ! command -v go >/dev/null 2>&1; then
    echo "error: go was not found on PATH." >&2
    exit 1
fi

step_clean() {
    echo "==> clean"
    rm -f "$out"
    rm -rf "$dist"
    go clean
}

step_test() {
    echo "==> gofmt"
    unformatted="$(gofmt -l "$root")"
    if [ -n "$unformatted" ]; then
        echo "These files need gofmt:"
        echo "$unformatted"
        exit 1
    fi
    echo "==> vet"
    go vet ./...
    echo "==> test"
    go test ./...
}

step_build() {
    echo "==> build"
    go build -trimpath -ldflags "-s -w" -o "$out" .
    echo "built $out"
}

step_cross() {
    # Same matrix the release workflow builds, for reproducing CI locally.
    mkdir -p "$dist"
    while read -r os arch ext; do
        name="winres-gen-${os}-${arch}${ext}"
        echo "==> ${name}"
        GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 \
            go build -trimpath -ldflags "-s -w" -o "${dist}/${name}" .
    done <<'TARGETS'
windows amd64 .exe
windows arm64 .exe
linux amd64 
linux arm64 
darwin amd64 
darwin arm64 
TARGETS
    echo "built $(ls -1 "$dist" | wc -l) binaries in $dist"
}

case "${1:-build}" in
clean) step_clean ;;
test) step_test ;;
build) step_build ;;
cross) step_cross ;;
all)
    step_clean
    step_test
    step_build
    ;;
*)
    echo "error: unknown target \"$1\" (expected: build, clean, test, all, cross)" >&2
    exit 1
    ;;
esac
