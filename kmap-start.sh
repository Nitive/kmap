#!/bin/bash

set -euo pipefail

cd /home/nitive/Develop/keyboard

device="${KEYBOARD_DEVICE:-/dev/input/by-path/platform-i8042-serio-0-event-kbd}"
config="${KMAP_CONFIG:-${ALTREMAP_CONFIG:-/home/nitive/Develop/keyboard/kmap.yaml}}"

exec /home/nitive/Develop/keyboard/bin/kmap start --device "$device" --config "$config"
