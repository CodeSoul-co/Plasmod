#!/bin/sh
# Optional corporate / custom TLS roots for `go mod` and HTTPS inside the plasmod container.
# Mount PEM certificates into /plasmod-custom-ca (see docker-compose.yml).
set -e
if [ -d /plasmod-custom-ca ]; then
	n=0
	for f in /plasmod-custom-ca/*; do
		[ -f "$f" ] || continue
		case "$f" in
		*.crt) ;;
		*.pem) ;;
		*) continue ;;
		esac
		n=$((n + 1))
		cp "$f" "/usr/local/share/ca-certificates/plasmod-custom-$(basename "$f")"
	done
	if [ "$n" -gt 0 ]; then
		update-ca-certificates >/dev/null
	fi
fi
exec "$@"
