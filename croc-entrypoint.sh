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
for argument do
    case "$argument" in
        serve|relay)
            service=$argument
            break
            ;;
    esac
done

relay_ports=${CROC_RELAY_PORTS:-${CROC_PORTS:-}}

case "$service" in
    serve)
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

exec /croc "$@"
