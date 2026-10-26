#!/bin/sh
# prompt.sh — line-oriented terminal wrapper for the servicetalk
# example shell (examples/shell/Main.smt).
#
# Reads stdin one line at a time, POSTs to /sh, prints the response.
# Type `exit` (locally) to quit; the cluster's job objects keep running.
#
# Usage:
#   ./smt coordinator --port 8000 --main Main \
#       --path examples/shell &
#   ./examples/shell/prompt.sh

URL="${SHELL_URL:-http://localhost:8080/sh}"

while :; do
    printf 'st-sh> '
    if ! IFS= read -r line; then
        echo
        break
    fi
    case "$line" in
        ''|exit|quit) [ "$line" = "" ] && continue || break ;;
    esac
    out=$(curl -sS --max-time 30 -X POST "$URL" --data-binary "$line")
    if [ -n "$out" ]; then
        printf '%s' "$out"
        case "$out" in
            *$'\n') ;;
            *) echo ;;
        esac
    fi
done
