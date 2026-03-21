#!/bin/bash

set -euo pipefail

cd /home/nitive/Develop/keyboard

device="${KEYBOARD_DEVICE:-/dev/input/by-path/platform-i8042-serio-0-event-kbd}"
config="${ALTREMAP_CONFIG:-/home/nitive/Develop/keyboard/altremap.yaml}"

exec /home/nitive/Develop/keyboard/bin/altremap --device "$device" --config "$config" # --generate-xcompose /home/nitive/.XCompose
