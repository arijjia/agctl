# @arijjia/agctl-workspace-console — консоль воркспейсов (сценарий 1c-vibe)

Клиент-плагин dsh web. Отдельная консоль вместо вкладки в диалоге создания
воркспейса: список проектов пользователя, у каждого — каталог компонентов
(навыки, инструменты, данные, промпты) с галочками включения. Настройки не
валиваются в момент создания воркспейса: сначала клонируется репка-донор,
сценарий настраивается потом и динамически.

## Контракты

- `GET /api/me` — чип консоли показывается только роли developer/admin.
- `GET /api/projects` — список воркспейсов-проектов.
- `GET /api/projects/{name}/components` — каталог компонентов: id, kind
  (skill|tool|data|docs), title, description, path, enabled, locked.
  Истина о состоянии — рабочее дерево проекта.
- `PUT /api/projects/{name}/components` с `{"disabled":[...]}` — применить
  выбор на сервере: выключенные компоненты переносятся в
  `/workspace/<name>/.agctl/stash/` (stash не виден агенту и не коммитится
  autosave'ом), включённые возвращаются. Репка-донор не правится.

## Реализация и проверка

`lib/client.js` — hand-written Lazy-CJS bundle, React/DSH API не форкаются.
Чип регистрируется в штатном слоте `sidebar.footer.action` (order 11, под
чипом кабинета). Тесты: `node --test dsh-plugins/workspace-console/test/`.
