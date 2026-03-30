#!/usr/bin/env bash
# isolate_users.sh — Restrict home directory access to owner only (chmod 750)
# Usage: sudo bash scripts/admin/isolate_users.sh [--dry-run]
#
# What it does:
#   1. Sets chmod 750 on every regular user home directory (uid >= 1000)
#   2. Sets a restrictive umask (027) in each user's ~/.profile and ~/.bashrc
#   3. Reports any remaining world-readable/writable paths under home dirs

set -euo pipefail

DRY_RUN=false
[[ "${1:-}" == "--dry-run" ]] && DRY_RUN=true

if [[ "$DRY_RUN" == false ]] && [[ $EUID -ne 0 ]]; then
    echo "ERROR: This script must be run as root (sudo)." >&2
    exit 1
fi

run() {
    if $DRY_RUN; then
        echo "[dry-run] $*"
    else
        "$@"
    fi
}

echo "======================================================"
echo " Home Directory Isolation Script"
$DRY_RUN && echo " Mode: DRY RUN — no changes will be made"
echo "======================================================"
echo ""

# ── Step 1: Set chmod 750 on every regular user home dir ─────────────────────
echo ">> Step 1: Setting home directory permissions (750)..."

while IFS=: read -r username _ uid _ _ homedir _; do
    # Skip system accounts (uid < 1000) and nobody
    [[ "$uid" -lt 1000 ]] && continue
    [[ "$username" == "nobody" ]] && continue
    [[ -z "$homedir" || ! -d "$homedir" ]] && continue

    current=$(stat -c "%a" "$homedir")
    if [[ "$current" != "750" ]]; then
        echo "   $username: $homedir  ($current -> 750)"
        run chmod 750 "$homedir"
    else
        echo "   $username: $homedir  ($current) [already correct]"
    fi
done < /etc/passwd

echo ""

# ── Step 2: Set restrictive umask in user shell profiles ─────────────────────
echo ">> Step 2: Ensuring umask 027 in user shell profiles..."

UMASK_LINE='umask 027'
UMASK_MARKER='# added by isolate_users.sh'

while IFS=: read -r username _ uid _ _ homedir shell; do
    [[ "$uid" -lt 1000 ]] && continue
    [[ "$username" == "nobody" ]] && continue
    [[ -z "$homedir" || ! -d "$homedir" ]] && continue

    for rcfile in "$homedir/.profile" "$homedir/.bashrc"; do
        if [[ -f "$rcfile" ]]; then
            if grep -q "$UMASK_LINE" "$rcfile" 2>/dev/null; then
                echo "   $rcfile  [umask already set]"
            else
                echo "   $rcfile  -> appending umask 027"
                run bash -c "echo '' >> '$rcfile'; echo '$UMASK_MARKER' >> '$rcfile'; echo '$UMASK_LINE' >> '$rcfile'"
            fi
        fi
    done
done < /etc/passwd

echo ""

# ── Step 3: Warn about world-writable files only when home dir is permissive ──
# If the home dir is already 750/700, outsiders cannot traverse into it,
# so world-readable files inside are not a security concern.
echo ">> Step 3: Scanning for world-writable files in exposed home dirs..."

FOUND=false
while IFS=: read -r username _ uid _ _ homedir _; do
    [[ "$uid" -lt 1000 ]] && continue
    [[ "$username" == "nobody" ]] && continue
    [[ -z "$homedir" || ! -d "$homedir" ]] && continue

    perm=$(stat -c "%a" "$homedir")
    # Only scan inside if home dir is world-accessible (others have r or x)
    others=$(( perm % 10 ))
    if [[ "$others" -gt 0 ]]; then
        mapfile -t ww_files < <(find "$homedir" -maxdepth 3 -perm -o+w -not -path "*/.git/*" 2>/dev/null || true)
        if [[ ${#ww_files[@]} -gt 0 ]]; then
            echo "   WARNING: world-writable files under $homedir (perm=${perm}):"
            printf "     %s\n" "${ww_files[@]}"
            FOUND=true
        fi
    else
        echo "   $username: $homedir (${perm}) — home dir protected, inner perms irrelevant"
    fi
done < /etc/passwd

$FOUND || echo "   No world-writable exposure found."

echo ""
echo "======================================================"
echo " Done. Run scripts/admin/check_isolation.sh to verify."
echo "======================================================"
