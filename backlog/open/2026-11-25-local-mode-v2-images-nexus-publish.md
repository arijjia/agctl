---
schema: 1.5.0
id: local-mode-v2-images-nexus-publish
title: "Локальный режим v2: публикация вкусов web/1С в Nexus из профиля"
status: open
priority: P2
size: M
area: platform
repo: arijjia/agctl
created: 2026-11-25
updated: 2026-11-25
tags: ["local", "docker", "images", "registry"]
depends: []
sessions:
  - session-1b497025-c44d-4733-bb3d-7d70ab937a0a:work
not-before: 2026-12-09
---

## Описание

Хвост карточки `local-mode-v2-container-bridge` (ветка agent/local-mode-v2 влита в main).
Код `agctl dev` готов, go vet/test зелёные. Не выполнен один критерий той
карточки: «Образы web и 1С публикуются в реестр; разработчик ничего не собирает».

Факты репо (проверены 2026-11-25):
- цель пула существует: `make img_push REGISTRY=<реестр>/agctl` (`bin/k8s.mk`) —
  тегирует и пушит собранные локальные образы, включая вкус web (`IMG_WEB`) и
  вкусы профиля (`PROFILE_FLAVORS`, для utmn — 1С); без `REGISTRY` падает с
  «✗ задай REGISTRY=<реестр>/agctl»;
- теги образов фиксированы в `bin/img.mk`: `agctl-dsh:0.2.2`, `agctl-dsh-web:0.2.2`,
  вкус профиля — `<overlay>-<flavor>:0.2.2` (`flavor_tag`); эталон digest — вывод
  `docker image inspect --format '{{.RepoDigests}}'` сразу после `img_push`;
- каталог образов лаунчера developer'а — поле `images.dev` в `profile.yaml`
  (`internal/profile/profile.go`, `DevImages`; флаг `serve --dev-images` /
  env `AGCTL_DEV_IMAGES`); туда вписываются полные ссылки на реестр;
- репо профиля: сиблинг `../agctl-profile-utmn` (`PROFILE_REPO` в
  `bin/profile.mk`, для CI/релиза — git-URL), подтягивается `make profile-fetch`;
  правка `profile.yaml` делается в репо профиля, не в agctl;
- `bin/e2e-local-mode.sh` уже умеет работать без pull/сборки (`dev init --no-pull`
  + готовый образ), отдельного режима «без сборки» добавлять не нужно.

Вводная, которой нет в репо (нужна от владельца/организации): адрес Nexus.
Запасной вариант для проверки механики без Nexus — локальный реестр
`docker run -d -p 5000:5000 registry:2` и `REGISTRY=localhost:5000/agctl`.

## Критерии готовности

- [ ] В `profile.yaml` организации (пилот — utmn) поле `images.dev` содержит
      полные ссылки на образы в реестре организации (базовый, web, 1С).
- [ ] `make img_push REGISTRY=<адрес>/agctl` проходит, digest'ы из вывода
      `docker image inspect --format '{{.RepoDigests}}'` для `agctl-dsh-web` и вкуса
      1С совпадают с тем, что отдаёт `docker manifest inspect` по тем же ссылкам.
- [ ] На чистой машине с Docker `agctl dev init` + `agctl dev up DIR` поднимается
      только на pull: `bin/e2e-local-mode.sh` без шага `make img_build` зелёный,
      локально отсутствуют образы `agctl-dsh*` до запуска.
- [ ] Критерий «Образы web и 1С публикуются в реестр» в карточке
      local-mode-v2-container-bridge закрыт со ссылкой на эту карточку.

## Журнал

- 2026-11-25: gate: ready (попытка 1)
- 2026-11-25: gate passed, feedback cleared
- 2026-11-25: unpark: —
- 2026-11-25: hold not-before 2026-12-09 (ждём адрес Nexus организации от владельца: без него критерии 1–3 невыполнимы, тур упрётся в тот же блокер)
