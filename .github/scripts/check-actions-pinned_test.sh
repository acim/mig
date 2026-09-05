#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
check_script="$script_dir/check-actions-pinned.sh"
fixture_dir="$(mktemp -d)"
trap 'rm -rf "$fixture_dir"' EXIT

expect_failure() {
  if "$@"; then
    echo "command unexpectedly succeeded: $*" >&2
    exit 1
  fi
}

expect_reference_failure() {
  local reference="$1"
  local message="$2"

  printf 'steps:\n  - uses: %s\n' "$reference" >"$fixture_dir/unsafe.yaml"
  if output=$("$check_script" "$fixture_dir/unsafe.yaml" 2>&1); then
    echo "reference unexpectedly succeeded: $reference" >&2
    exit 1
  fi
  if [[ "$output" != *"$message"* ]]; then
    printf 'unexpected failure for %s:\n%s\n' "$reference" "$output" >&2
    exit 1
  fi
}

expect_default_failure() {
  local directory="$1"
  if (cd "$directory" && "$check_script"); then
    echo "pinning check unexpectedly succeeded in: $directory" >&2
    exit 1
  fi
}

printf '%s\n' \
  'steps:' \
  '  - uses: ./local-action' \
  '  - uses: docker://alpine:3.23' \
  '  - uses: ectobit/reusable-workflows/.github/workflows/go-check.yaml@main' \
  '  - uses: actions/checkout@v7' \
  '  - uses: raven-actions/actionlint@v2' \
  '  - uses: "actions/setup-go@v7"' \
  "  - run: 'echo \"uses: harmless/example@main\"'" \
  '  # uses: commented/example@main' \
  >"$fixture_dir/safe.yaml"

"$check_script" "$fixture_dir/safe.yaml"

expect_reference_failure \
  'ectobit/reusable-workflows/.github/workflows/go-check.yaml@3d3c42e5aac5ba805825da76410c181273ba90b1' \
  'first-party ectobit reference must use @main'
expect_reference_failure \
  'ectobit/reusable-workflows/.github/workflows/go-check.yaml@v1' \
  'first-party ectobit reference must use @main'

for reference in \
  'actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1' \
  'actions/checkout@v7.0.1' \
  'actions/checkout@main'; do
  expect_reference_failure "$reference" 'third-party reference must use a major version tag'
done

printf '%s\n' \
  'steps:' \
  '  - uses:' \
  '      actions/checkout@main' \
  >"$fixture_dir/multiline.yaml"
expect_failure "$check_script" "$fixture_dir/multiline.yaml"

mkdir -p "$fixture_dir/empty/.github/workflows" "$fixture_dir/missing"
expect_default_failure "$fixture_dir/empty"
expect_default_failure "$fixture_dir/missing"
