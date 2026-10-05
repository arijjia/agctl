#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""lint.py — BSL Language Server как CLI проекта (fail-closed).

Скачивает закреплённый релиз (если jar нет), гоняет analyze по каталогу
и валидирует отчёт: пустой/битый/без структуры = ошибка (код 1).
Ошибки уровня Error1..Error4 в отчёте — тоже код 1 (warnings не роняют).

Запуск:
    python3 lint.py --src <каталог с BSL> [--out <каталог отчёта>] [--strict]
Без --strict (по умолчанию) ненулевой код выхода только при отсутствии
отчёта/ошибке запуска; с --strict любая диагностика Error роняет проверку.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import subprocess
import sys
import urllib.request
from pathlib import Path

BSL_LS_VERSION = "1.0.7"
BSL_LS_URL = (f"https://github.com/1c-syntax/bsl-language-server/releases/"
              f"download/v{BSL_LS_VERSION}/bsl-language-server-{BSL_LS_VERSION}-exec.jar")
# SHA-256 уточняется первым скачиванием и закрепляется здесь (см. README);
# пусто = только размер >0 (первый прогон на новой машине).
BSL_LS_SHA256 = ""
MIN_JAR_BYTES = 1_000_000  # настоящий jar на порядки больше


def log(message: str) -> None:
    print(f"[lint] {message}", file=sys.stderr, flush=True)


def jar_path() -> Path:
    cache = Path(os.environ.get("BSL_LS_JAR", Path.home() / ".cache" / "1c-vibe" / "bsl-ls.jar"))
    cache.parent.mkdir(parents=True, exist_ok=True)
    if cache.exists() and cache.stat().st_size >= MIN_JAR_BYTES:
        return cache
    log(f"скачиваю BSL Language Server {BSL_LS_VERSION}...")
    request = urllib.request.Request(BSL_LS_URL, headers={"User-Agent": "1c-vibe"})
    with urllib.request.urlopen(request, timeout=300) as response:
        data = response.read()
    if len(data) < MIN_JAR_BYTES:
        raise RuntimeError(f"скачанный jar подозрительно мал: {len(data)} байт")
    digest = hashlib.sha256(data).hexdigest()
    if BSL_LS_SHA256 and digest != BSL_LS_SHA256:
        raise RuntimeError(f"SHA-256 не совпал: {digest}")
    if not BSL_LS_SHA256:
        log(f"закрепи SHA-256 в lint.py: {digest}")
    cache.write_bytes(data)
    log(f"jar сохранён: {cache} ({len(data)} байт)")
    return cache


def java_available() -> bool:
    try:
        subprocess.run(["java", "-version"], capture_output=True, timeout=30)
        return True
    except (OSError, subprocess.TimeoutExpired):
        return False


def parse_report(report_file: Path) -> tuple[int, int]:
    """(всего диагностик, ошибок уровня Error). Битый отчёт = ValueError."""
    with open(report_file, encoding="utf-8") as handle:
        data = json.load(handle)
    total = errors = 0
    for entry in data if isinstance(data, list) else []:
        severity = str(entry.get("severity", "")).lower()
        total += 1
        if severity.startswith("error") or severity == "критичнаяошибка":
            errors += 1
    return total, errors


def main() -> int:
    parser = argparse.ArgumentParser(description="BSL-линтер проекта (fail-closed)")
    parser.add_argument("--src", required=True, help="каталог с BSL/XML выгрузкой")
    parser.add_argument("--out", default="", help="каталог для отчёта (по умолчанию <src>/.bsl-ls)")
    parser.add_argument("--strict", action="store_true",
                        help="ненулевой код выхода при любых диагностиках Error")
    args = parser.parse_args()

    src = Path(args.src).resolve()
    if not src.is_dir():
        print(f"нет каталога: {src}")
        return 1
    out_dir = Path(args.out).resolve() if args.out else src / ".bsl-ls"
    out_dir.mkdir(parents=True, exist_ok=True)

    if not java_available():
        print("Java не найдена — линт невозможен (fail-closed)")
        return 1

    jar = jar_path()
    log("анализ...")
    completed = subprocess.run(
        ["java", "-jar", str(jar), "analyze", "-s", str(src), "-o", str(out_dir), "-r", "json"],
        capture_output=True, text=True, timeout=600,
    )
    report = out_dir / "bsl-json.json"
    if not report.exists() or report.stat().st_size == 0:
        print("ОТЧЁТ ОТСУТСТВУЕТ ИЛИ ПУСТ — fail-closed")
        print(completed.stdout[-2000:])
        print(completed.stderr[-2000:])
        return 1

    try:
        total, errors = parse_report(report)
    except (json.JSONDecodeError, ValueError) as error:
        print(f"ОТЧЁТ ПОВРЕЖДЁН: {error} — fail-closed")
        return 1

    print(f"диагностик: {total}, ошибок Error: {errors}")
    print(f"отчёт: {report}")
    if args.strict and errors:
        print("STRICT: есть ошибки Error — проверка не пройдена")
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
