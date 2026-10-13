#!/bin/sh
# dsh-bump: сверка пина @deepseek-ai/dsh в internal/assets/image/Dockerfile
# с dist-tags npm. Карточка backlog: dsh-pin-bump-automation.
#
#   scripts/dsh-bump.sh --check     # exit 1, если пин != latest
#   scripts/dsh-bump.sh --update    # обновить pinned Dockerfile до latest rc/stable
#   scripts/dsh-bump.sh             # просто показать текущий пин и latest
#
# Политика: пинуем rc/stable; alpha/beta/next не ставим. После --update
# собрать образ и прогнать e2e: make img_build && FAKE_LLM=1 scripts/e2e-smoke.sh.
set -eu
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DOCKERFILE="${DSH_BUMP_DOCKERFILE:-$REPO_ROOT/internal/assets/image/Dockerfile}"

PINNED="$(sed -n 's/.*@deepseek-ai\/dsh@\([0-9][^" ]*\).*/\1/p' "$DOCKERFILE" | head -1)"
if [ -z "$PINNED" ]; then
  echo "dsh-bump: не нашёл пин @deepseek-ai/dsh в $(basename "$DOCKERFILE")" >&2
  exit 2
fi

LATEST="$(npm view @deepseek-ai/dsh dist-tags.latest 2>/dev/null)"
if [ -z "$LATEST" ]; then
  echo "dsh-bump: npm недоступен, latest неизвестен" >&2
  exit 2
fi

echo "pinned: $PINNED"
echo "latest: $LATEST"

case "${1:-}" in
--update)
  case "$LATEST" in
  *-alpha.*|*-beta.*|*-next.*|*-dev.*)
    echo "dsh-bump: refusing prerelease tag '$LATEST' outside rc policy" >&2
    exit 2
    ;;
  esac
  if [ "$PINNED" = "$LATEST" ]; then
    echo "dsh-bump: пин уже актуален"
    exit 0
  fi
  python3 - "$DOCKERFILE" "$PINNED" "$LATEST" <<'PY'
from pathlib import Path
import sys
path, old, new = sys.argv[1:]
p = Path(path)
text = p.read_text()
needle = f"@deepseek-ai/dsh@{old}"
if text.count(needle) != 1:
    raise SystemExit(f"dsh-bump: expected one {needle!r} in {path}")
p.write_text(text.replace(needle, f"@deepseek-ai/dsh@{new}"))
PY
  echo "dsh-bump: pin обновлён до $LATEST; далее: make img_build && FAKE_LLM=1 scripts/e2e-smoke.sh"
  ;;
--check)
  if [ "$PINNED" = "$LATEST" ]; then
    echo "dsh-bump: пин актуален"
    exit 0
  fi
  case "$LATEST" in
  *-alpha.*|*-beta.*|*-next.*|*-dev.*)
    echo "dsh-bump: latest=$LATEST — prerelease, авто-дрейф допускается"
    exit 0
    ;;
  esac
  echo "dsh-bump: ДРИФТ — scripts/dsh-bump.sh --update, затем make img_build и e2e-смоук" >&2
  exit 1
  ;;
*)
  if [ "$PINNED" != "$LATEST" ]; then
    echo "dsh-bump: есть дрейф; выполните scripts/dsh-bump.sh --update для rc/stable"
  fi
  ;;
esac
