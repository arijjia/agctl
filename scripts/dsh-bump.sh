#!/bin/sh
# dsh-bump: сверка пина @deepseek-ai/dsh в internal/assets/image/Dockerfile
# с dist-tags npm. Карточка backlog: dsh-pin-bump-automation.
#
#   scripts/dsh-bump.sh --check     # exit 1, если пин != latest (для CI)
#   scripts/dsh-bump.sh             # просто показать текущий пин и latest
#
# Политика: пинуем rc/stable; alpha не ставим автоматически. Апгрейд —
# вручную: правка пина + make img_build + e2e-смоук (см. hq-гайд).
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

case "$1" in
--check)
  if [ "$PINNED" = "$LATEST" ]; then
    echo "dsh-bump: пин актуален"
    exit 0
  fi
  case "$LATEST" in
  *-alpha.*)
    echo "dsh-bump: latest=$LATEST — alpha, авто-дрифт допускаем (политика: только rc/stable)"
    exit 0
    ;;
  esac
  echo "dsh-bump: ДРИФТ — обнови пин и прогони e2e-смоук (make img_build; hq e2e guide)" >&2
  exit 1
  ;;
*)
  if [ "$PINNED" != "$LATEST" ]; then
    echo "dsh-bump: есть дрейф; политика — rc/stable, alpha не ставим автоматически"
  fi
  ;;
esac
