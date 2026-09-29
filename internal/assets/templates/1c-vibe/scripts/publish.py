#!/usr/bin/env python3
"""Безопасная публикация правок агента: ветка → commit → MR → Jira.

Работа разделена на две команды:

  python publish.py ISSUE --repo PATH --prepare
  python publish.py ISSUE "краткое резюме" --repo PATH

Сначала ``--prepare`` создаёт чистую ветку ``agent/ISSUE`` от основной ветки.
После того как агент внёс и проверил изменения, вторая команда создаёт коммит,
пушит ветку, открывает Merge Request и комментирует задачу Jira.
"""

import argparse
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import tempfile
import urllib.parse

from envconfig import load_env
from gitlab_client import api as gitlab_api, encoded_path
from jira_client import api as jira_api

load_env()

ISSUE_KEY_RE = re.compile(r"^[A-Z][A-Z0-9_]*-\d+$")
URL_CREDENTIALS_RE = re.compile(r"(https?://)[^/@\s]+@", re.IGNORECASE)


def redact(text: str) -> str:
    """Удаляет токены и credentials из текста перед выводом в лог."""
    safe = URL_CREDENTIALS_RE.sub(r"\1***@", text)
    for name in ("GITLAB_TOKEN", "JIRA_TOKEN"):
        secret = os.environ.get(name)
        if secret:
            safe = safe.replace(secret, "***")
    return safe


def git(repo: str, *args: str, env: dict[str, str] | None = None) -> str:
    result = subprocess.run(
        ["git", "-C", repo, *args],
        capture_output=True,
        text=True,
        env=env,
    )
    if result.returncode != 0:
        operation = args[0] if args else "command"
        detail = redact(result.stderr.strip() or result.stdout.strip())
        sys.exit(f"git {operation} завершился с ошибкой:\n{detail}")
    return result.stdout.strip()


def validate_issue_key(issue: str) -> str:
    normalized = issue.upper()
    if not ISSUE_KEY_RE.fullmatch(normalized):
        sys.exit(
            f"Некорректный ключ задачи: {issue!r}. Ожидается формат INTAIDEV-101."
        )
    return normalized


def remote_project(repo: str) -> tuple[str, str, str]:
    """Возвращает ``(scheme, host, project_path)`` для origin."""
    origin = git(repo, "remote", "get-url", "origin")
    if origin.startswith("git@"):
        host, path = origin[4:].split(":", 1)
        scheme = "ssh"
    else:
        parsed = urllib.parse.urlsplit(origin)
        if parsed.scheme not in {"http", "https", "ssh"} or not parsed.hostname:
            sys.exit(f"Неподдерживаемый формат origin: {redact(origin)}")
        scheme = parsed.scheme
        host = parsed.hostname
        if parsed.port:
            host = f"{host}:{parsed.port}"
        path = parsed.path.lstrip("/")

    project_path = path.removesuffix(".git")
    if not project_path or project_path.startswith("/") or ".." in project_path.split("/"):
        sys.exit(f"Не удалось определить путь GitLab-проекта из origin: {redact(origin)}")
    return scheme, host, project_path


def assert_gitlab_host(remote_host: str) -> None:
    """Не даёт отправить GitLab-токен на хост из подменённого origin."""
    gitlab_url = os.environ.get("GITLAB_URL", "")
    parsed = urllib.parse.urlsplit(gitlab_url)
    configured_host = parsed.hostname or ""
    if parsed.port:
        configured_host = f"{configured_host}:{parsed.port}"
    if remote_host.lower() != configured_host.lower():
        sys.exit(
            "Хост origin не совпадает с GITLAB_URL: "
            f"origin={remote_host}, GITLAB_URL={configured_host}. Push остановлен."
        )


def project_and_base(repo: str, requested_base: str | None) -> tuple[dict, str, str]:
    _scheme, host, project_path = remote_project(repo)
    assert_gitlab_host(host)
    project = gitlab_api("GET", f"/projects/{encoded_path(project_path)}")
    if not isinstance(project, dict):
        sys.exit("GitLab вернул неожиданный ответ при чтении проекта.")
    base = requested_base or project.get("default_branch") or "main"
    return project, base, project_path


def ensure_clean(repo: str) -> None:
    if git(repo, "status", "--porcelain"):
        sys.exit(
            "Рабочая копия содержит изменения. Сохрани или убери их, затем повтори "
            "подготовку ветки с --prepare."
        )


def prepare(repo: str, issue: str, requested_base: str | None) -> None:
    ensure_clean(repo)
    git(repo, "fetch", "origin", "--prune")
    _project, base, _project_path = project_and_base(repo, requested_base)
    branch = f"agent/{issue}"
    git(repo, "checkout", "-B", branch, f"origin/{base}")
    print(f"Ветка готова: {branch} (от origin/{base}).")
    print("Теперь агент может вносить изменения и запускать проверку.")


def push_branch(repo: str, branch: str, scheme: str) -> None:
    """Пушит через SSH или HTTPS askpass, не помещая токен в argv/URL."""
    if scheme == "ssh":
        git(repo, "push", "--set-upstream", "origin", f"{branch}:{branch}")
        return

    token = os.environ.get("GITLAB_TOKEN")
    if not token:
        sys.exit("GITLAB_TOKEN не задан: заполни mvp/.env")

    with tempfile.TemporaryDirectory(prefix="1c-vibe-askpass-") as temp_dir:
        askpass = Path(temp_dir) / "git-askpass.sh"
        askpass.write_text(
            "#!/bin/sh\n"
            "case \"$1\" in\n"
            "  *Username*) printf '%s\\n' oauth2 ;;\n"
            "  *Password*) printf '%s\\n' \"$GITLAB_TOKEN\" ;;\n"
            "  *) exit 1 ;;\n"
            "esac\n",
            encoding="utf-8",
        )
        askpass.chmod(stat.S_IRUSR | stat.S_IWUSR | stat.S_IXUSR)
        push_env = os.environ.copy()
        push_env.update(
            {
                "GIT_ASKPASS": str(askpass),
                "GIT_TERMINAL_PROMPT": "0",
                "GIT_CONFIG_COUNT": "1",
                "GIT_CONFIG_KEY_0": "credential.helper",
                "GIT_CONFIG_VALUE_0": "",
            }
        )
        git(
            repo,
            "push",
            "--set-upstream",
            "origin",
            f"{branch}:{branch}",
            env=push_env,
        )


def find_open_mr(project_id: int, branch: str) -> dict | None:
    query = urllib.parse.urlencode(
        {"state": "opened", "source_branch": branch, "per_page": 1}
    )
    result = gitlab_api("GET", f"/projects/{project_id}/merge_requests?{query}")
    if isinstance(result, list) and result:
        return result[0]
    return None


def submit(
    repo: str,
    issue: str,
    summary: str,
    requested_base: str | None,
) -> None:
    project, base, project_path = project_and_base(repo, requested_base)
    scheme, _host, parsed_project_path = remote_project(repo)
    if parsed_project_path != project_path:
        sys.exit("Путь origin изменился во время выполнения; публикация остановлена.")

    branch = f"agent/{issue}"
    current_branch = git(repo, "branch", "--show-current")
    if current_branch != branch:
        sys.exit(
            f"Текущая ветка {current_branch!r}, ожидалась {branch!r}. "
            f"Сначала выполни: publish.py {issue} --repo {repo} --prepare"
        )

    git(repo, "add", "-A")
    if git(repo, "status", "--porcelain"):
        git(repo, "commit", "-m", f"feat({issue}): {summary}")
        print("[1/4] коммит создан")
    else:
        ahead = git(repo, "rev-list", "--count", f"origin/{base}..HEAD")
        if ahead == "0":
            sys.exit("Изменений и неопубликованных коммитов нет.")
        print(f"[1/4] новых изменений нет; используем готовые коммиты: {ahead}")

    push_branch(repo, branch, scheme)
    print(f"[2/4] ветка запушена: {branch}")

    mr = find_open_mr(project["id"], branch)
    if mr:
        print(f"[3/4] используем уже открытый MR: {mr['web_url']}")
    else:
        jira_url = os.environ.get("JIRA_URL", "").rstrip("/")
        mr = gitlab_api(
            "POST",
            f"/projects/{project['id']}/merge_requests",
            {
                "source_branch": branch,
                "target_branch": base,
                "title": f"{issue}: {summary}",
                "description": (
                    f"{summary}\n\nЗадача: {jira_url}/browse/{issue}\n\n"
                    "Изменения подготовлены ИИ-агентом 1c-vibe и требуют ревью человека."
                ),
                "remove_source_branch": True,
            },
        )
        if not isinstance(mr, dict) or "web_url" not in mr:
            sys.exit("GitLab вернул неожиданный ответ при создании MR.")
        print(f"[3/4] MR создан: {mr['web_url']}")

    jira_api(
        "POST",
        f"/rest/api/2/issue/{urllib.parse.quote(issue, safe='')}/comment",
        {
            "body": (
                "Агент 1c-vibe подготовил решение.\n\n"
                f"Что сделано: {summary}\n"
                f"Merge Request: {mr['web_url']}\n\n"
                "После ревью и вливания MR задачу можно закрыть."
            )
        },
    )
    print(f"[4/4] комментарий добавлен в {issue}")
    print(f"\nГотово. MR: {mr['web_url']}")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("issue", help="ключ задачи, например INTAIDEV-101")
    parser.add_argument("summary", nargs="?", help="резюме правки одним предложением")
    parser.add_argument("--repo", required=True, help="путь к клону демо-репозитория")
    parser.add_argument("--base", default=None, help="базовая ветка (по умолчанию из GitLab)")
    parser.add_argument(
        "--prepare",
        action="store_true",
        help="подготовить чистую рабочую ветку до редактирования",
    )
    args = parser.parse_args()

    issue = validate_issue_key(args.issue)
    repo = os.path.abspath(args.repo)
    if not os.path.isdir(os.path.join(repo, ".git")):
        sys.exit(f"{repo} — не git-репозиторий; укажи клон через --repo")

    if args.prepare:
        if args.summary:
            parser.error("для --prepare резюме не нужно")
        prepare(repo, issue, args.base)
        return

    if not args.summary or not args.summary.strip():
        parser.error("для публикации требуется резюме; либо используй --prepare")
    submit(repo, issue, args.summary.strip(), args.base)


if __name__ == "__main__":
    main()
