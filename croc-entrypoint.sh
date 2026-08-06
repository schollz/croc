#!/bin/sh
set -e

has_option() {
    option=$1
    shift
    for argument do
        case "$argument" in
            "$option"|"$option"=*)
                return 0
                ;;
        esac
    done
    return 1
}

service=
case "${1:-}" in
    web|croc-web|serve)
        service=web
        shift
        ;;
esac

if [ -z "$service" ]; then
    for argument do
        case "$argument" in
            relay)
                service=relay
                break
                ;;
        esac
    done
fi

relay_ports=${CROC_RELAY_PORTS:-${CROC_PORTS:-}}

case "$service" in
    web)
        if [ -n "${STORE_DIR:-}" ] && ! has_option --store-dir "$@"; then
            set -- "$@" --store-dir "$STORE_DIR"
        fi
        if [ -n "$relay_ports" ] && ! has_option --ports "$@"; then
            set -- "$@" --ports "$relay_ports"
        fi
        if [ -n "${SITE_URL:-}" ]; then
            set -- "$@" "$SITE_URL"
        fi
        ;;
    relay)
        if [ -n "$relay_ports" ] && ! has_option --ports "$@"; then
            set -- "$@" --ports "$relay_ports"
        fi
        ;;
esac

if [ -n "$CROC_PASS" ]; then
    set -- --pass "$CROC_PASS" "$@"
fi

if [ "$service" = web ]; then
    exec /croc-web "$@"
fi

exec /croc "$@"
