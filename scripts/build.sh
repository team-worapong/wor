#!/usr/bin/env sh
set -eu

usage() {
	echo "Usage: $0 [goos goarch]" >&2
	echo "Examples:" >&2
	echo "  $0" >&2
	echo "  $0 linux amd64" >&2
	echo "  $0 linux arm64" >&2
	echo "  $0 darwin arm64" >&2
	echo "  $0 windows amd64" >&2
}

case "$#" in
	0)
		target_goos="$(go env GOOS)"
		target_goarch="$(go env GOARCH)"
		;;
	2)
		target_goos="$1"
		target_goarch="$2"
		;;
	*)
		usage
		exit 2
		;;
esac

case "$target_goos" in
	linux | darwin | windows) ;;
	*)
		echo "unsupported GOOS: $target_goos" >&2
		exit 2
		;;
esac

case "$target_goarch" in
	amd64 | arm64) ;;
	*)
		echo "unsupported GOARCH: $target_goarch" >&2
		exit 2
		;;
esac

binary_name="wor"
if [ "$target_goos" = "windows" ]; then
	binary_name="wor.exe"
fi

output_dir="dist/${target_goos}-${target_goarch}"
output="${output_dir}/${binary_name}"

mkdir -p "$output_dir"

GOOS="$target_goos" GOARCH="$target_goarch" go build -o "$output" ./cmd/wor

echo "Built $output"
