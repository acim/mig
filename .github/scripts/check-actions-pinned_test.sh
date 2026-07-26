#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
check_script="$script_dir/check-actions-pinned.sh"
fixture_dir="$(mktemp -d)"
trap 'rm -rf "$fixture_dir"' EXIT

printf '%s\n' \
  'steps:' \
  '  - uses: ./local-action' \
  '  - uses: docker://alpine:3.23' \
  '  - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1' \
  '  - uses: raven-actions/actionlint@v2' \
  >"$fixture_dir/safe.yaml"

"$check_script" "$fixture_dir/safe.yaml"

printf '%s\n' \
  'jobs:' \
  '  check:' \
  '    uses: owner/repository/.github/workflows/check.yaml@main' \
  >"$fixture_dir/unsafe.yaml"

if "$check_script" "$fixture_dir/unsafe.yaml"; then
  echo "mutable external reference passed the pinning check" >&2
  exit 1
fi
