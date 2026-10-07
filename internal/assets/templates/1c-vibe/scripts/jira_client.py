#!/usr/bin/env python3
"""Мини-клиент Jira для MVP 1c-vibe.

Умеет:
  get    ISSUE-KEY          — показать постановку задачи;
  comment ISSUE-KEY "текст" — добавить комментарий;
  search "JQL"              — найти задачи (ключ, статус, метки).

Аутентификация: Personal Access Token (Jira Server / Data Center),
заголовок `Authorization: Bearer <JIRA_TOKEN>`.
Если у вас Jira Cloud — замените `_headers()` на Basic-авторизацию
(email + API-токен), остальное не меняется.

Настройки (в mvp/.env, см. .env.example):
  JIRA_URL   — например https://jira.example.local
  JIRA_TOKEN — персональный токен
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

API = "/rest/api/2"


def _base() -> str:
    url = os.environ.get("JIRA_URL")
    if not url:
        sys.exit("JIRA_URL не задан: заполни mvp/.env (см. .env.example)")
    if "example.local" in url:
        sys.exit("JIRA_URL — заглушка из .env.example: впиши реальный адрес Jira в mvp/.env")
    return url.rstrip("/")


def _headers() -> dict:
    """Basic (JIRA_USER + JIRA_PASSWORD) или Bearer PAT (JIRA_TOKEN).

    Basic удобен для Server/DC, когда PAT ещё не выпущен: логин = короткий
    доменный, пароль — доменный. Приоритет: Basic, если заданы оба поля.
    """
    user = os.environ.get("JIRA_USER", "")
    password = os.environ.get("JIRA_PASSWORD", "")
    if user and password:
        import base64
        creds = base64.b64encode(f"{user}:{password}".encode("utf-8")).decode("ascii")
        return {
            "Authorization": f"Basic {creds}",
            "Content-Type": "application/json",
        }
    token = os.environ.get("JIRA_TOKEN")
    if not token:
        sys.exit("не задана авторизация: JIRA_USER+JIRA_PASSWORD или JIRA_TOKEN")
    return {
        "Authorization": f"Bearer {token}",
        "Content-Type": "application/json",
    }


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
    req = urllib.request.Request(_base() + path, data=data, headers=_headers(), method=method)
    try:
        with urllib.request.urlopen(req, timeout=_timeout(), context=_ssl_context()) as resp:
            body = resp.read().decode("utf-8")
            return json.loads(body) if body else {}
    except urllib.error.HTTPError as e:
        detail = e.read().decode("utf-8", "replace")[:500]
        sys.exit(f"Jira ответила {e.code}: {detail}")
    except urllib.error.URLError as e:
        sys.exit(
            f"Не удалось соединиться с {_base()}: {e.reason}\n"
            "Проверь JIRA_URL в mvp/.env (адрес доступен? VPN поднят?)"
        )


def description_text(value) -> str:
    """Читабельно разворачивает строку Jira Server или ADF из Jira Cloud."""
    if value is None:
        return "(описания нет)"
    if isinstance(value, str):
        return value
    if isinstance(value, dict):
        own_text = value.get("text")
        children = value.get("content", [])
        chunks = [own_text] if isinstance(own_text, str) else []
        chunks.extend(description_text(child) for child in children)
        separator = "\n" if value.get("type") in {"doc", "bulletList", "listItem"} else ""
        return separator.join(chunk for chunk in chunks if chunk and chunk != "(описания нет)")
    if isinstance(value, list):
        return "\n".join(description_text(item) for item in value)
    return str(value)


def get_issue(key: str) -> None:
    encoded_key = urllib.parse.quote(key, safe="")
    issue = api("GET", f"{API}/issue/{encoded_key}")
    if not isinstance(issue, dict):
        sys.exit("Jira вернула неожиданный ответ при чтении задачи")
    fields = issue.get("fields", {})
    print(f"{issue.get('key')}: {fields.get('summary', '')}")
    print("-" * 60)
    print(description_text(fields.get("description")))


def add_comment(key: str, text: str) -> None:
    encoded_key = urllib.parse.quote(key, safe="")
    api("POST", f"{API}/issue/{encoded_key}/comment", {"body": text})
    print(f"Комментарий добавлен к {key}.")


def create_issue(project: str, summary: str, description: str,
                 issue_type: str = "Задача", labels: list[str] | None = None) -> str:
    """Создать задачу в проекте; вернуть ключ (например INTAIDEV-4)."""
    fields: dict = {
        "project": {"key": project},
        "summary": summary,
        "description": description,
        "issuetype": {"name": issue_type},
    }
    if labels:
        fields["labels"] = labels
    created = api("POST", f"{API}/issue", {"fields": fields})
    if not isinstance(created, dict) or "key" not in created:
        sys.exit("Jira вернула неожиданный ответ при создании задачи")
    return str(created["key"])


def search_issues(jql: str, max_results: int = 20) -> list[dict]:
    """Поиск задач по JQL; вернуть [{key, summary, status, labels}]."""
    query = urllib.parse.urlencode({"jql": jql, "maxResults": max_results})
    data = api("GET", f"{API}/search?{query}")
    if not isinstance(data, dict) or "issues" not in data:
        sys.exit("Jira вернула неожиданный ответ при поиске задач")
    out: list[dict] = []
    for issue in data.get("issues", []):
        fields = issue.get("fields", {})
        out.append({
            "key": issue.get("key", ""),
            "summary": fields.get("summary", ""),
            "status": (fields.get("status") or {}).get("name", ""),
            "labels": list(fields.get("labels") or []),
        })
    return out


def main() -> None:
    if len(sys.argv) < 3:
        print(__doc__)
        sys.exit(2)
    cmd, key = sys.argv[1], sys.argv[2]
    if cmd == "get":
        get_issue(key)
    elif cmd == "comment" and len(sys.argv) >= 4:
        add_comment(key, sys.argv[3])
    elif cmd == "search":
        for issue in search_issues(key):
            labels = ",".join(issue["labels"])
            print(f"{issue['key']} [{issue['status']}] ({labels}): {issue['summary']}")
    else:
        print(__doc__)
        sys.exit(2)


if __name__ == "__main__":
    main()
