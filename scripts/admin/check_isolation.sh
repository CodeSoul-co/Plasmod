#!/usr/bin/env bash
# check_isolation.sh — Verify home directory isolation and sudo group status
# Usage: bash scripts/admin/check_isolation.sh
# No root required for most checks.

set -euo pipefail

PASS=0
FAIL=0

ok()   { echo "  [PASS] $*"; ((PASS++)); }
fail() { echo "  [FAIL] $*"; ((FAIL++)); }
info() { echo "  [INFO] $*"; }

echo "======================================================"
echo " Isolation Check Report"
echo " $(date)"
echo "======================================================"

# ── Check 1: Home directory permissions ──────────────────────────────────────
echo ""
echo ">> [1] Home directory permissions (expected: 750)"

while IFS=: read -r username _ uid _ _ homedir _; do
    [[ "$uid" -lt 1000 ]] && continue
    [[ "$username" == "nobody" ]] && continue
    [[ -z "$homedir" || ! -d "$homedir" ]] && continue

    perm=$(stat -c "%a" "$homedir")
    owner=$(stat -c "%U" "$homedir")

    if [[ "$perm" == "750" || "$perm" == "700" ]]; then
        ok "$username: $homedir  (${perm}) owner=${owner}"
    else
        fail "$username: $homedir  (${perm}) — expected 750 or 700, owner=${owner}"
    fi
done < /etc/passwd

# ── Check 2: sudo group membership ───────────────────────────────────────────
echo ""
echo ">> [2] sudo group membership"

sudo_members=$(getent group sudo | cut -d: -f4)
info "Current sudo members: ${sudo_members:-none}"

ALLOWED_SUDO="ninghanwen duanzhenke"
IFS=',' read -ra members <<< "$sudo_members"
for member in "${members[@]}"; do
    [[ -z "$member" ]] && continue
    if echo "$ALLOWED_SUDO" | grep -qw "$member"; then
        ok "$member is in sudo group (expected)"
    else
        fail "$member is in sudo group (unexpected — consider removing)"
    fi
done

# ── Check 3: No world-writable files in home dirs ────────────────────────────
echo ""
echo ">> [3] World-writable files in home directories"

FOUND=false
while IFS=: read -r username _ uid _ _ homedir _; do
    [[ "$uid" -lt 1000 ]] && continue
    [[ "$username" == "nobody" ]] && continue
    [[ -z "$homedir" || ! -d "$homedir" ]] && continue
    [[ ! -r "$homedir" ]] && info "Cannot read $homedir (access denied — isolation working)" && continue

    mapfile -t ww_files < <(find "$homedir" -maxdepth 3 -perm -o+w -not -path "*/.git/*" 2>/dev/null || true)
    if [[ ${#ww_files[@]} -gt 0 ]]; then
        fail "World-writable files under $homedir:"
        printf "       %s\n" "${ww_files[@]}"
        FOUND=true
    fi
done < /etc/passwd

$FOUND || ok "No world-writable files found"

# ── Check 4: Cross-user directory access ─────────────────────────────────────
echo ""
echo ">> [4] Cross-user access test (current user: $(whoami))"

CURRENT=$(whoami)
while IFS=: read -r username _ uid _ _ homedir _; do
    [[ "$uid" -lt 1000 ]] && continue
    [[ "$username" == "nobody" ]] && continue
    [[ "$username" == "$CURRENT" ]] && continue
    [[ -z "$homedir" || ! -d "$homedir" ]] && continue

    if ls "$homedir" &>/dev/null; then
        fail "$(whoami) CAN list $homedir — isolation incomplete"
    else
        ok "$(whoami) CANNOT access $homedir (isolation working)"
    fi
done < /etc/passwd

# ── Check 5: umask in shell profiles ─────────────────────────────────────────
echo ""
echo ">> [5] umask settings in shell profiles"

CURRENT_UMASK=$(umask)
if [[ "$CURRENT_UMASK" == "0027" || "$CURRENT_UMASK" == "027" ]]; then
    ok "Current session umask: $CURRENT_UMASK"
else
    info "Current session umask: $CURRENT_UMASK (run isolate_users.sh to set 027)"
fi

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "======================================================"
echo " Summary: ${PASS} passed, ${FAIL} failed"
if [[ $FAIL -eq 0 ]]; then
    echo " Status: OK — isolation configured correctly"
else
    echo " Status: ACTION REQUIRED — run sudo bash scripts/admin/isolate_users.sh"
fi
echo "======================================================"
exit $FAIL
