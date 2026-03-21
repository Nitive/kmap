#!/bin/bash

set -euo pipefail

cd /home/nitive/Develop/keyboard

device="${KEYBOARD_DEVICE:-}"
config="${KMAP_CONFIG:-/home/nitive/Develop/keyboard/kmap.yaml}"

args=(start --config "$config")
if [[ -n "$device" ]]; then
  args+=(--device "$device")
fi

exec /home/nitive/Develop/keyboard/bin/kmap "${args[@]}"
