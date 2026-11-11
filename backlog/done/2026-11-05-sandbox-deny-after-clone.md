---
schema: 1.1.0
id: sandbox-deny-after-clone
title: "Landlock-песочница dsh отрицает доступ к свежесклонированному воркспейсу"
status: done
priority: P2
size: M
area: server
repo: arijjia/agctl
created: 2026-11-05
updated: 2026-11-12
tags: [dsh, sandbox]
depends: []
epic: ""
sessions:
  - session-f61b3c76-c5b1-4184-b046-86a58e758c75:work
---
## Описание

Кейс 2026-11-05 (разраб, воркспейс voproshalych, склонирован через диалог
chat-brand → POST /api/projects {cloneUrl}): агент в сессии воркспейса
(cwd /workspace/voproshalych) получил на `ls` своей же рабочей папки:
`Permission denied` + `[sandbox: file access denied under workspace-write
mode]`. Воспроизводится и после перезапуска контейнера/агента (турн в 08:53
упал после рестарта 08:46).

Что исключено проверками на живом стенде:
- права FS: в контейнере всё root:root, агент и лаунчер landlock ходят под
  root (root игнорирует 0700); `docker exec -u node ls` тоже читает;
- статус DeveloperToolsPreference / сборка бандла — включены и свежие;
- существование каталога на момент спавна команды — существует.

Значит: landlock-гранты writableRoots(policy) = [workspaceRoot, /tmp, tmp]
не покрывают каталог при спавне. Подозреваемые: policy.workspaceRoot
зафиксирован на момент СОЗДАНИЯ воркспейса и не перечитывается после
клонирования/удаления+рекреации каталога (инод менялся), либо
canonicalPath/реальный путь расходится.

## План

1. Локализовать: в контейнере залогировать policy.workspaceRoot и
   проматчить с путём отказа для этой сессии (трейс: u3,
   --workspace-voproshalych--/session-a556cd01).
2. Если root фиксируется на старом иноде — триггерить пересоздание гранта
   при workspace.create/replace (upstream dsh или патч).
3. Убедиться, что диагностика отказа песочницы пишет целевой путь гранта.

## Критерии готовности

- Клонированный воркспейс доступен агенту в workspace-write без
  перезапуска сессии; повтор delete→re-clone тоже.

## Журнал

- 2026-11-05: создано; обходчик для пользователя — скачивание через
  «Воркспейсы: возможности → Файлы» (работает) и/или новая сессия в
  воркспейсе (свежий бинд песочницы).
- 2026-11-12: Диагноз (воспроизведено в контейнере agctl-dsh:0.2.2 на этом хосте): гипотеза про инод после клона НЕ подтвердилась — landlock отрицает доступ к /workspace с первого же гранта, без всякого clone/delete. Корень: bind-маунты Docker Desktop для macOS имеют тип fakeowner (virtiofs), на котором правила Landlock не применяются: probe лаунчера проходит по локальной ФС (‘fully enforced’), ранг выбирается, а любой confined-доступ под /workspace = EPERM. Bash-инструмент dsh матчит ‘permission denied’ как policy-denial (‘do not retry another way’) — инфра-сбой замаскирован под отказ политики. Гипотеза карточки о workspaceRoot/иноде снята: writableRoots резолвятся на каждый вызов из session.header.cwd. Фикс agctl: entrypoint образа функционально проверяет landlock против $HOME (probe + ls под грантом) и на несовместимом маунте ставит DSH_PERMISSION_MODE=danger-full-access (штатный deployment-оверрайд dsh, cordis.patch.yml dsh-base) с громким логом; граница изоляции — сам контейнер. Проверено: смоук-контейнер с маунтом из macOS — лог срабатывает, dsh web стартует; negative control (явный DSH_PERMISSION_MODE) не перекрывается; на локальных для контейнера путях проверка проходит (прогон лаунчера). Источники: живой прогон лаунчера в контейнере, dsh 0.2.1-rc.5 (dsh-sandbox-local/dsh-bash-sandbox/cordis.patch.yml) — сессии-источники карточки не читал, факты подтверждены воспроизведением.
- 2026-11-12: refl: Карточка начинала с гипотезы «инод менялся после clone» и плана патчить upstream — живое воспроизведение за 10 минут показало другой корень (virtiofs-маунт), лечащийся на стороне agctl без патча dsh. Урок: дорогая диагностика по описанию симптомов дешевле одного контролируемого эксперимента в контейнере.
- 2026-11-12: done — Landlock-детект в entrypoint образа: на хостах, где песочница dsh не может конфайнить $HOME (Docker Desktop virtiofs/fakeowner), режим уходит в danger-full-access с громким логом; на Linux поведение не меняется
