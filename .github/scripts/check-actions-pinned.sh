#!/usr/bin/env bash

set -euo pipefail

if (( $# == 0 )); then
  mapfile -d '' files < <(find .github/workflows -type f \( -name '*.yml' -o -name '*.yaml' \) -print0)
else
  files=("$@")
fi

failed=0
for file in "${files[@]}"; do
  line_number=0
  while IFS= read -r line || [[ -n "$line" ]]; do
    ((line_number += 1))
    if [[ ! "$line" =~ uses:[[:space:]]*([^[:space:]#]+) ]]; then
      continue
    fi

    reference="${BASH_REMATCH[1]}"
    case "$reference" in
      ./* | docker://* | raven-actions/actionlint@v2)
        continue
        ;;
    esac

    if [[ ! "$reference" =~ @[0-9a-fA-F]{40}$ ]]; then
      printf '%s:%d: mutable external Actions reference: %s\n' "$file" "$line_number" "$reference" >&2
      failed=1
    fi
  done <"$file"
done

exit "$failed"
