#!/usr/bin/env bash
# Validates the frontmatter of every skill and subagent with Claude Code's own parser, the one that decides whether they load.
# A file that fails here does not fail at runtime: it loads with empty metadata or is skipped, and nothing says so.

set -u

# The CLI is not on every PATH: with only the editor extension installed, the binary lives inside it and its folder carries the version.
if command -v claude >/dev/null 2>&1; then
  cli=claude
else
  cli=$(ls -d "$HOME"/.vscode/extensions/anthropic.claude-code-*/resources/native-binary/claude* 2>/dev/null | sort | tail -n 1)
fi

if [ -z "${cli:-}" ]; then
  echo "Claude Code not found: install the CLI or the VS Code extension, then run this again." >&2
  exit 1
fi

root=$(git rev-parse --show-toplevel 2>/dev/null) || root=.
status=0

for dir in .claude/skills .claude/agents; do
  [ -d "$root/$dir" ] || continue
  "$cli" plugin validate "$root/$dir" || status=1
done

exit "$status"
