#!/bin/sh
set -e

if [ -n "$CROC_PASS" ]; then
    set -- --pass "$CROC_PASS" "$@"
fi

exec /croc "$@"
