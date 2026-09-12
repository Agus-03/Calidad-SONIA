#!/usr/bin/env bash
# PreToolUse hook of the read-only subagents: Bash may run only `git <subcommand>` for the subcommands given as arguments.
# It lives here because a subagent's `tools` field takes tool names only, so `Bash(git diff:*)` there restricts nothing.
# Exit 2 blocks the call, and Claude Code hands stderr back to the subagent as the reason.

set -u

allowed="$*"
input=$(tr -d '\r\n')

# Parsed with sed because jq and python are not on every machine of the team; only the first unescaped quote ends the value.
command=$(printf '%s' "$input" | sed -nE 's/.*"command"[[:space:]]*:[[:space:]]*"(([^"\\]|\\.)*)".*/\1/p')

deny() {
  echo "Blocked: this subagent may only run git ${allowed// /, } with plain arguments. $1" >&2
  exit 2
}

[ -n "$command" ] || deny "The command could not be read."

# Plain words only: no quoting, expansion, redirection or chaining, so what is checked is exactly what runs.
case "$command" in
  *[!A-Za-z0-9_./:@^~=+,%*\ -]*) deny "Only letters, digits, spaces and _ . / : @ ^ ~ = + , % * - are allowed." ;;
esac

set -f
# shellcheck disable=SC2086 # the split into words is the point, and globbing is off
set -- $command

[ "${1:-}" = git ] || deny "The command has to start with git."
shift
[ "${1:-}" = --no-pager ] && shift

sub="${1:-}"
case " $allowed " in
  *" $sub "*) [ -n "$sub" ] || deny "A git subcommand is missing." ;;
  *) deny "git ${sub} is not one of them." ;;
esac
shift

# The two ways these subcommands write a file or run a program: a read-only reviewer needs neither.
for arg in "$@"; do
  case "$arg" in
    --output*|--ext-diff) deny "$arg writes a file or runs an external program." ;;
  esac
done

exit 0
