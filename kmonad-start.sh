#!/bin/bash

set -euo pipefail

tmp_dir="$(mktemp --directory)"
envsubst < /home/nitive/Develop/keyboard/kmonad.kbd > "$tmp_dir/kmonad.kbd"

/usr/bin/kmonad "$tmp_dir/kmonad.kbd"
