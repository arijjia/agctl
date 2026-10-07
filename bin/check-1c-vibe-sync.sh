#!/usr/bin/env bash
# check-1c-vibe-sync.sh — anti-drift сверка «репо 1c-vibe ↔ шаблон agctl».
# Живёт до T4 (provision тянет шаблоны из git-URL), запускать перед каждым
# релизом бинаря: `make tpl_check_sync`.
#
# Использование:
#   bin/check-1c-vibe-sync.sh [путь-к-1c-vibe]   # по умолчанию ../1c-vibe
#
# Правила сверки (единственные разрешённые расхождения):
#   - *.py, tests/, demo-project/, pyproject.toml — байт-в-байт;
#   - .env.example — кроме первых 2 строк шапки (mvp/.env vs scripts/.env);
#   - SKILL.md — кроме замены mvp/ → scripts/ (раскладка рабочего места).
# Плюс гигиена embed: под templates/1c-vibe нет __pycache__/.venv/.env/*.pyc.
set -euo pipefail

VIBE="${1:-${VIBE_REPO:-../1c-vibe}}"
TPL="$(cd "$(dirname "$0")/../internal/assets/templates/1c-vibe" && pwd)"
SRC="$VIBE/mvp"
DST="$TPL/scripts"
FAIL=0

say()  { printf '%s\n' "$*"; }
pass() { say "ok: $*"; }
fail() { say "FAIL: $*"; FAIL=1; }

[[ -d "$SRC" ]] || { say "FAIL: нет $SRC (укажи путь к checkout 1c-vibe)"; exit 1; }
[[ -d "$DST" ]] || { say "FAIL: нет $DST"; exit 1; }

# 1. *.py верхнего уровня — байт-в-байт
for f in "$SRC"/*.py; do
  base="$(basename "$f")"
  if [[ -f "$DST/$base" ]]; then
    if diff -q "$f" "$DST/$base" >/dev/null; then pass "$base"; else fail "$base разошёлся"; fi
  else
    fail "$base отсутствует в шаблоне"
  fi
done
# pyproject.toml — байт-в-байт
if diff -q "$SRC/pyproject.toml" "$DST/pyproject.toml" >/dev/null; then
  pass "pyproject.toml"
else
  fail "pyproject.toml разошёлся"
fi

# 2. tests/ и demo-project/ — рекурсивно, кроме python-кэша
for d in tests demo-project; do
  if diff -rq --exclude=__pycache__ --exclude='*.pyc' "$SRC/$d" "$DST/$d" >/dev/null; then
    pass "$d/"
  else
    fail "$d/ разошёлся:"; diff -rq --exclude=__pycache__ --exclude='*.pyc' "$SRC/$d" "$DST/$d" | head -n 10
  fi
done

# 3. .env.example — тело равно, шапка (первые 2 строки) своя
if diff -q <(tail -n +3 "$SRC/.env.example") <(tail -n +3 "$DST/.env.example") >/dev/null; then
  pass ".env.example (тело)"
else
  fail ".env.example (тело) разошёлся"
fi

# 4. SKILL.md — равна с точностью до замены mvp/ → scripts/
if diff -q <(sed 's|mvp/|scripts/|g' "$VIBE/mvp/skills/1c-vibe-demo/SKILL.md") \
          "$TPL/skills/1c-vibe-demo/SKILL.md" >/dev/null; then
  pass "SKILL.md (mvp→scripts)"
else
  fail "SKILL.md разошёлся шире замены mvp/→scripts/"
fi

# 5. обязательные файлы шаблона на месте
for f in AGENTS.md README.md skills/1c-vibe-demo/SKILL.md backlog/open/2026-09-28-1c-vibe-onboarding.md; do
  if [[ -f "$TPL/$f" ]]; then pass "$f"; else fail "нет $f в шаблоне"; fi
done

# 6. гигиена go:embed — мусор, раздувающий бинарь
junk="$(find "$TPL" \( -name __pycache__ -o -name .venv -o -name .env -o -name '*.pyc' \) -print | head -n 10)"
if [[ -z "$junk" ]]; then
  pass "embed-гигиена"
else
  fail "мусор под templates/1c-vibe:"; say "$junk"
fi

if [[ "$FAIL" -eq 0 ]]; then say "PASS: шаблон в синке с репо"; else say "FAIL: шаблон отстал — см. выше"; fi
exit "$FAIL"
