#!/bin/sh
# Optional corporate / custom TLS roots for `go mod` and HTTPS inside the andb container.
# Mount PEM certificates into /andb-custom-ca (see docker-compose.yml).
set -e
if [ -d /andb-custom-ca ]; then
	n=0
	for f in /andb-custom-ca/*; do
		[ -f "$f" ] || continue
		case "$f" in
		*.crt) ;;
		*.pem) ;;
		*) continue ;;
		esac
		n=$((n + 1))
		cp "$f" "/usr/local/share/ca-certificates/andb-custom-$(basename "$f")"
	done
	if [ "$n" -gt 0 ]; then
		update-ca-certificates >/dev/null
	fi
fi
exec "$@"
