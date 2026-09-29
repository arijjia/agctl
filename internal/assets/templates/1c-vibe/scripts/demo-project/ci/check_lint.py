#!/usr/bin/env python3
"""Fail-closed проверка JSON-отчёта BSL Language Server 1.0.x.

Пайплайн падает, если отчёт отсутствует/повреждён/имеет неизвестную структуру
или содержит диагностики уровня Error. Чистый отчёт без диагностик допустим.
"""

import json
from pathlib import Path
import sys

ERROR_VALUES = {"error", "1"}  # 1 = Error по Language Server Protocol


def diagnostics(report: object) -> list[dict]:
    if not isinstance(report, dict):
        raise ValueError("корень JSON должен быть объектом")
    fileinfos = report.get("fileinfos")
    if not isinstance(fileinfos, list):
        raise ValueError("в отчёте нет массива fileinfos")

    result = []
    for index, fileinfo in enumerate(fileinfos):
        if not isinstance(fileinfo, dict):
            raise ValueError(f"fileinfos[{index}] должен быть объектом")
        items = fileinfo.get("diagnostics")
        if not isinstance(items, list):
            raise ValueError(f"fileinfos[{index}].diagnostics должен быть массивом")
        for diagnostic in items:
            if not isinstance(diagnostic, dict) or "severity" not in diagnostic:
                raise ValueError("диагностика не содержит severity")
            result.append(diagnostic)
    return result


def check_report(report: object) -> tuple[int, int]:
    items = diagnostics(report)
    errors = [
        item
        for item in items
        if str(item["severity"]).strip().lower() in ERROR_VALUES
    ]
    return len(items), len(errors)


def main() -> None:
    path = Path(sys.argv[1] if len(sys.argv) > 1 else "bsl-reports/bsl-json.json")
    if not path.is_file():
        sys.exit(f"Отчёт BSL LS не создан: {path}")
    try:
        report = json.loads(path.read_text(encoding="utf-8"))
        total, errors = check_report(report)
    except (OSError, json.JSONDecodeError, ValueError) as error:
        sys.exit(f"Некорректный отчёт BSL LS: {error}")

    print(f"Диагностик: {total}, из них уровня Error: {errors}")
    if errors:
        sys.exit("Есть ошибки — смотрите артефакт bsl-reports/bsl-json.json.")
    print("Ошибок уровня Error нет.")


if __name__ == "__main__":
    main()
