#!/bin/sh
# dsh-bump: сверка пина @deepseek-ai/dsh (ARG DSH_VERSION в
# internal/assets/image/Dockerfile) с dist-tags npm. Карточки backlog:
# dsh-pin-bump-automation, dsh-bump-next-channel.
#
#   bin/dsh-bump.sh                          # пин и оба канала (latest, next)
#   bin/dsh-bump.sh --check                  # exit 1, если пин отстаёт от next
#   bin/dsh-bump.sh --check --channel latest # ворота только по latest
#   bin/dsh-bump.sh --update                 # поднять пин до next
#   bin/dsh-bump.sh --update --channel latest
#
# Канал по умолчанию — next (свежие сборки выходят там раньше; на
# 2026-11-13 latest отставал от next на две версии). rc-политика одна для
# обоих каналов: пинуем rc/stable, версии 1.2.3-next.1/alpha/beta/dev не
# ставим — dist-tag не отменяет политику версии. После --update собрать
# образ и прогнать e2e: make img_build && FAKE_LLM=1 bin/e2e-smoke.sh.
# Конвейер обновления dsh: воркфлоу dsh-bump еженедельно двигает пин PR'ом
# по latest и отдельно проверяет next (образ на ARG DSH_VERSION=<next>,
# e2e-first-session и тесты плагинов; при провале — issue, пин не двигается).
set -eu
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DOCKERFILE="${DSH_BUMP_DOCKERFILE:-$REPO_ROOT/internal/assets/image/Dockerfile}"

MODE=""
CHANNEL="next"
while [ $# -gt 0 ]; do
  case "$1" in
  --check|--update)
    MODE="$1"
    ;;
  --channel)
    shift
    case "${1:-}" in
    latest|next) CHANNEL="$1" ;;
    *) echo "dsh-bump: --channel принимает latest|next, получено '${1:-}'" >&2; exit 2 ;;
    esac
    ;;
  *)
    echo "dsh-bump: неизвестный аргумент '$1'" >&2
    exit 2
    ;;
  esac
  shift
done

PINNED="$(sed -n 's/^ARG DSH_VERSION=\([0-9][^" ]*\).*/\1/p' "$DOCKERFILE" | head -1)"
if [ -z "$PINNED" ]; then
  echo "dsh-bump: не нашёл ARG DSH_VERSION в $(basename "$DOCKERFILE")" >&2
  exit 2
fi

LATEST="$(npm view @deepseek-ai/dsh dist-tags.latest 2>/dev/null)"
NEXT="$(npm view @deepseek-ai/dsh dist-tags.next 2>/dev/null)"
if [ -z "$LATEST" ] || [ -z "$NEXT" ]; then
  echo "dsh-bump: npm недоступен, dist-tags неизвестны" >&2
  exit 2
fi

echo "pinned: $PINNED"
echo "latest: $LATEST"
echo "next:   $NEXT"

TARGET="$NEXT"
[ "$CHANNEL" = "latest" ] && TARGET="$LATEST"

# rc-политика версии, одинаковая для обоих каналов.
rc_refuses() {
  case "$1" in
  *-alpha.*|*-beta.*|*-next.*|*-dev.*) return 0 ;;
  *) return 1 ;;
  esac
}

OTHER="$LATEST"
[ "$CHANNEL" = "latest" ] && OTHER="$NEXT"

case "$MODE" in
--update)
  if rc_refuses "$TARGET"; then
    echo "dsh-bump: refusing prerelease tag '$TARGET' outside rc policy" >&2
    exit 2
  fi
  if [ "$PINNED" = "$TARGET" ]; then
    echo "dsh-bump: пин уже актуален (канал $CHANNEL)"
    exit 0
  fi
  python3 - "$DOCKERFILE" "$PINNED" "$TARGET" <<'PY'
from pathlib import Path
import sys
path, old, new = sys.argv[1:]
p = Path(path)
text = p.read_text()
needle = f"ARG DSH_VERSION={old}"
if text.count(needle) != 1:
    raise SystemExit(f"dsh-bump: expected one {needle!r} in {path}")
p.write_text(text.replace(needle, f"ARG DSH_VERSION={new}"))
PY
  echo "dsh-bump: пин обновлён до $TARGET (канал $CHANNEL); далее: make img_build && FAKE_LLM=1 bin/e2e-smoke.sh"
  ;;
--check)
  if [ "$PINNED" = "$TARGET" ]; then
    if [ "$PINNED" != "$OTHER" ] && ! rc_refuses "$OTHER"; then
      echo "dsh-bump: канал $CHANNEL актуален (другой канал впереди: $OTHER)"
    else
      echo "dsh-bump: пин актуален"
    fi
    exit 0
  fi
  if rc_refuses "$TARGET"; then
    echo "dsh-bump: $CHANNEL=$TARGET — prerelease, авто-дрейф допускается"
    exit 0
  fi
  echo "dsh-bump: ДРИФТ — $CHANNEL на $TARGET, пин $PINNED; bin/dsh-bump.sh --update, затем make img_build и e2e-смоук" >&2
  exit 1
  ;;
*)
  if [ "$PINNED" != "$TARGET" ]; then
    echo "dsh-bump: есть дрейф ($CHANNEL на $TARGET); bin/dsh-bump.sh --update для rc/stable"
  fi
  ;;
esac
