"""Загрузчик mvp/.env — крошечный, без внешних зависимостей.

Формат: KEY=VALUE, по паре на строку; '#' в начале строки — комментарий.
Значения из .env не перекрывают уже существующие переменные окружения.
"""

import os
from pathlib import Path


def load_env(path=None) -> None:
    env_path = Path(path) if path else Path(__file__).resolve().parent / ".env"
    if not env_path.exists():
        return
    for raw in env_path.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        os.environ.setdefault(key.strip(), value.strip().strip("'").strip('"'))
