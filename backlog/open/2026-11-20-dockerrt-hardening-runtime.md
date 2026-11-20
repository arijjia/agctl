---
schema: 1.0.0
id: dockerrt-hardening-runtime
title: "dockerrt: упрочнение контейнера и выбор рантайма runsc/kata для серверного режима"
status: open
priority: P2
size: M
area: platform
repo: arijjia/agctl
created: 2026-11-20
updated: 2026-11-20
tags: ["server", "docker", "sandbox", "security"]
depends: []
---
## Описание

Серверный Docker-режим — самый слабый по изоляции среди серверных
бэкендов: `dockerrt.Start` создаёт обычный runc-контейнер на общем ядре
(лимиты только память/CPU), тогда как k8s уже даёт non-root, cap drop ALL,
seccomp `RuntimeDefault` и опционально gVisor (`--k8s-runtime-class`), а AX —
gVisor/microVM. Главный вектор по security-brief — эксплойт ядра из
контейнера; лечится пользовательским ядром (gVisor) или microVM (Kata).

Docker Sandboxes (`sbx`) для сервера не подходят: инструмент рабочей станции
с входом одного Docker-аккаунта, без локального API и серверных сценариев
(см. `docker-sandboxes-local-runtime`). Ту же модель берём штатными
средствами Docker.

Состав:
- Флаг `serve --docker-runtime` (`AGCTL_DOCKER_RUNTIME`): пусто = runc,
  `runsc` (gVisor), `kata` → `HostConfig.Runtime`; проверка наличия
  рантайма в `docker info` при старте с понятной ошибкой.
- Упрочнение по умолчанию: `CapDrop: ALL` + минимальный `CapAdd` для sshd
  (как в k8srt), `no-new-privileges`, `PidsLimit`, при возможности
  non-root — не ломая entrypoint/sshd.
- Совместимость с локальным режимом (тот же код): упрочнение не должно
  ломать Docker Desktop/Colima; рантайм в локальном режиме не выставляем.

## Критерии готовности

- [ ] `--docker-runtime runsc` проверен на Linux-хосте: чат, SSH developer'а,
      egress-сеть, сон/пробуждение.
- [x] Упрочнение включено по умолчанию, e2e (`bin/e2e-smoke.sh`,
      `bin/e2e-ssh.sh`, `bin/e2e-local-mode.sh`) зелёные.
- [x] `docs/architecture.md` §4.2 и §10.4 обновлены.

## Журнал

- 2026-11-20: создано (`gitctl backlog new`) по итогам исследования Docker
  Sandboxes.
- 2026-11-20: Реализовано (ветка agent/dockerrt-hardening): `dockerrt/harden.go` —
  CapDrop ALL + CHOWN/DAC_OVERRIDE/FOWNER/FSETID/SETGID/SETUID/SYS_CHROOT/KILL/
  AUDIT_WRITE (non-root не включали: entrypoint chown'ит тома хоста, sshd с
  privsep), `no-new-privileges`, PidsLimit 4096; флаги agctl-server
  `--docker-runtime` (проверка по docker info при старте), `--docker-pids-limit`,
  `--docker-harden=0` (откат); локальный режим получает упрочнение без рантайма.
  Юнит-тесты harden_test.go; смоук проверяет инспектом CapDrop/SecurityOpt/
  PidsLimit. Docker Desktop (macOS): e2e-ssh 16/0, e2e-local-mode 44/0,
  e2e-smoke 39/0 CHAT=PASS. Открыто: `--docker-runtime runsc` на Linux-хосте
  с gVisor (чат, SSH, egress-сеть, сон/пробуждение) — здесь нет такого хоста.
