#!/usr/bin/env bash
#
# Re-runs the README's prettycov blocks and diffs them against the tool.
#
# Three README sections have shipped numbers from a retired fixture — the profile testdata holds two
# of, differing by golang/go#80974's statement inflation, which look equally plausible on the page.
# Reading them is no defence; running them is.
#
# Only verbatim blocks: a command carrying its result inline, or output with a row elided or a
# comment beside it, is illustration and is skipped rather than guessed at.
set -uo pipefail
BIN=${1:?}; PROFILE=${2:?}; fail=0; checked=0
while IFS=: read -r ln cmd; do
  args=${cmd#❯ prettycov}
  # An elided command, or one carrying its result inline, is illustration rather than a paste.
  case "$args" in *"…"*|*"  "*) continue;; esac
  want=$(awk -v n="$ln" 'NR>n{ if ($0=="```") exit; print }' README.md)
  # So is a block that elides rows or annotates them.
  case "$want" in *"…"*|*"❯"*|*"#"*) continue;; esac
  got=$(eval "$BIN -profile $PROFILE $args" 2>&1)
  checked=$((checked+1))
  if [ "$want" != "$got" ]; then
    echo "MISMATCH README:$ln  prettycov$args"
    diff <(printf '%s\n' "$want") <(printf '%s\n' "$got") | sed 's/^/    /' | head -6
    fail=1
  fi
done < <(grep -n "^❯ prettycov" README.md)
echo "checked $checked verbatim blocks against $PROFILE"
exit $fail
