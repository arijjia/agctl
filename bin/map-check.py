#!/usr/bin/env python3
"""map-check — проверка карты системы без archify (вызывается из `make map`).

Что проверяется:
  1. спецификация *.architecture.json читается, все ссылки (views.focus,
     boundaries.wraps, connections.from/to) указывают на существующие узлы;
  2. улики (components[].sources): файл существует, line/end_line в пределах
     файла — чтобы карта не ссылалась на уехавшие строки;
  3. свежесть HTML: sha256 спецификации сравнивается с receipt archify
     (*.delivery.json) — если не совпадает, HTML собран из другой версии.

Выход: 0 — всё в порядке; 1 — ошибки в спецификации/уликах;
2 — спецификация новее HTML (нужна пересборка: make map_build).
"""
import hashlib
import json
import sys
from pathlib import Path


def main(argv):
    if len(argv) != 3:
        print("usage: map-check.py <spec.architecture.json> <delivery.json>", file=sys.stderr)
        return 1
    spec_path, receipt_path = Path(argv[1]), Path(argv[2])
    root = Path(__file__).resolve().parent.parent
    spec = json.loads(spec_path.read_text(encoding="utf-8"))

    errors = []
    ids = {c["id"] for c in spec.get("components", [])}
    for v in spec.get("meta", {}).get("views", []):
        errors += [f"view {v['id']}: нет узла {x}" for x in v.get("focus", []) if x not in ids]
    for b in spec.get("boundaries", []):
        errors += [f"рамка «{b.get('label')}»: нет узла {x}" for x in b.get("wraps", []) if x not in ids]
    for c in spec.get("connections", []):
        errors += [f"связь {c.get('id')}: нет узла {x}" for x in (c.get("from"), c.get("to")) if x not in ids]

    checked = 0
    for comp in spec.get("components", []):
        for s in comp.get("sources", []):
            checked += 1
            f = root / s["path"]
            if not f.is_file():
                errors.append(f"{comp['id']}: нет файла {s['path']}")
                continue
            n = len(f.read_text(encoding="utf-8", errors="replace").split("\n"))
            end = s.get("end_line", s.get("line", 1))
            if end > n:
                errors.append(f"{comp['id']}: {s['path']}:{s.get('line')}-{end} за концом файла ({n} строк)")

    if errors:
        print("карта: ошибки спецификации/улик:")
        for e in errors:
            print("  -", e)
        return 1

    print(f"карта: спецификация ок — узлов {len(ids)}, улик {checked}, "
          f"пин {spec.get('meta', {}).get('repository', {}).get('revision', '?')[:7]}")

    if receipt_path.is_file():
        receipt = json.loads(receipt_path.read_text(encoding="utf-8"))
        built = receipt.get("specification", {}).get("sha256")
        actual = hashlib.sha256(spec_path.read_bytes()).hexdigest()
        if built != actual:
            print("карта: ВНИМАНИЕ — HTML собран из другой версии спецификации; "
                  "пересборка: make map_build (нужен archify)")
            return 2
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
