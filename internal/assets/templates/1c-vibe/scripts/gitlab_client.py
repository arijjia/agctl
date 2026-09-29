#!/usr/bin/env python3
"""Мини-клиент GitLab для MVP 1c-vibe.

Умеет:
  whoami                                     — проверить токен;
  project  group/subgroup/project            — найти проект (id, web_url);
  create-mr group/subgroup/project SOURCE TITLE [DESCRIPTION]
          — создать merge request из ветки SOURCE в основную ветку.

Аутентификация: Personal Access Token, заголовок `PRIVATE-TOKEN`.
Настройки (в mvp/.env, см. .env.example):
  GITLAB_URL   — например https://gitlab.example.local
  GITLAB_TOKEN — токен со scope api
"""

import json
import os
import ssl
import sys
import urllib.error
import urllib.parse
import urllib.request

from envconfig import load_env

load_env()

API = "/api/v4"


def _base() -> str:
    url = os.environ.get("GITLAB_URL")
    if not url:
        sys.exit("GITLAB_URL не задан: заполни mvp/.env (см. .env.example)")
    if "example.local" in url:
        sys.exit("GITLAB_URL — заглушка из .env.example: впиши реальный адрес GitLab в mvp/.env")
    return url.rstrip("/")


def _headers() -> dict:
    token = os.environ.get("GITLAB_TOKEN")
    if not token:
        sys.exit("GITLAB_TOKEN не задан: заполни mvp/.env (см. .env.example)")
    return {"PRIVATE-TOKEN": token, "Content-Type": "application/json"}


def _timeout() -> float:
    try:
        return float(os.environ.get("HTTP_TIMEOUT_SECONDS", "30"))
    except ValueError:
        sys.exit("HTTP_TIMEOUT_SECONDS должен быть числом")


def _ssl_context() -> "ssl.SSLContext":
    """TLS-контекст: certifi как CA по умолчанию, плюс HTTPS_CA_BUNDLE / INSECURE_TLS."""
    try:
        import certifi

        context = ssl.create_default_context(cafile=certifi.where())
    except ImportError:
        context = ssl.create_default_context()
    bundle = os.environ.get("HTTPS_CA_BUNDLE")
    if bundle:
        if not os.path.isfile(bundle):
            sys.exit(f"HTTPS_CA_BUNDLE указывает не на файл: {bundle}")
        context.load_verify_locations(bundle)
    if os.environ.get("INSECURE_TLS", "").lower() in {"1", "true", "yes"}:
        context.check_hostname = False
        context.verify_mode = ssl.CERT_NONE
        print("ВНИМАНИЕ: проверка TLS-сертификата отключена (INSECURE_TLS=1)", file=sys.stderr)
    return context


def api(method: str, path: str, payload: dict | None = None) -> dict | list:
    data = json.dumps(payload).encode("utf-8") if payload is not None else None
    api_path = path if path.startswith("/") else f"/{path}"
    req = urllib.request.Request(
        _base() + API + api_path,
        data=data,
        headers=_headers(),
        method=method,
    )
    try:
        with urllib.request.urlopen(req, timeout=_timeout(), context=_ssl_context()) as resp:
            body = resp.read().decode("utf-8")
            return json.loads(body) if body else {}
    except urllib.error.HTTPError as e:
        detail = e.read().decode("utf-8", "replace")[:500]
        sys.exit(f"GitLab ответил {e.code}: {detail}")
    except urllib.error.URLError as e:
        sys.exit(
            f"Не удалось соединиться с {_base()}: {e.reason}\n"
            "Проверь GITLAB_URL в mvp/.env (адрес доступен? VPN поднят?)"
        )


def encoded_path(project: str) -> str:
    return urllib.parse.quote(project, safe="")


def whoami() -> None:
    user = api("GET", "/user")
    if not isinstance(user, dict):
        sys.exit("GitLab вернул неожиданный ответ при проверке токена")
    print(f"{user.get('username')} <{user.get('email', '?')}> — токен работает")


def get_project(project: str) -> None:
    info = api("GET", f"/projects/{encoded_path(project)}")
    if not isinstance(info, dict):
        sys.exit("GitLab вернул неожиданный ответ при чтении проекта")
    print(f"id={info['id']}  {info['path_with_namespace']}  {info['web_url']}")
    print(f"ветка по умолчанию: {info.get('default_branch')}")


def create_mr(project: str, source: str, title: str, description: str) -> dict:
    info = api("GET", f"/projects/{encoded_path(project)}")
    if not isinstance(info, dict):
        sys.exit("GitLab вернул неожиданный ответ при чтении проекта")
    target = info.get("default_branch") or "main"
    mr = api(
        "POST",
        f"/projects/{info['id']}/merge_requests",
        {
            "source_branch": source,
            "target_branch": target,
            "title": title,
            "description": description,
            "remove_source_branch": True,
        },
    )
    if not isinstance(mr, dict):
        sys.exit("GitLab вернул неожиданный ответ при создании MR")
    return mr


def main() -> None:
    argv = sys.argv[1:]
    if not argv:
        print(__doc__)
        sys.exit(2)
    if argv[0] == "whoami":
        whoami()
    elif argv[0] == "project" and len(argv) >= 2:
        get_project(argv[1])
    elif argv[0] == "create-mr" and len(argv) >= 4:
        project, source, title = argv[1], argv[2], argv[3]
        description = argv[4] if len(argv) >= 5 else ""
        mr = create_mr(project, source, title, description)
        print(f"MR !{mr['iid']}: {mr['web_url']}")
    else:
        print(__doc__)
        sys.exit(2)


if __name__ == "__main__":
    main()
