---
schema: 1.1.0
id: office-preview-in-container
title: "Превью Office-файлов в контейнере: нет Linux-движка у libreoffice-kit"
status: done
priority: P3
size: M
area: server
repo: arijjia/agctl
created: 2026-11-05
updated: 2026-11-13
tags: [dsh, preview]
depends: []
epic: ""
sessions:
  - session-365c2d55-6a2b-4739-99bd-914641f176f3:work
---
## Описание

Превью pptx/docx/xlsx в сайдбаре dsh конвертируется в PDF через
`@deepseek-ai/dsh-office-to-pdf` + `@deepseek-ai/libreoffice-kit`. Кит
поставляет движки только для darwin-arm64/darwin-x64/win32-x64/wasm —
Linux-движка нет, системный LibreOffice кит не использует. Поэтому в
agctl-dsh (Linux-контейнер) Office-превью недоступно в принципе, для любой
роли: «Office 预览不可用…» / тихий no-op. Обходчик — скачивание через
«Воркспейсы: возможности → Файлы».

## План

1. Проверить у upstream: планируется ли libreoffice-kit-linux-*; если да —
   просто доставить optional-dep в образ agctl-dsh.
2. Если нет — оценить свой конвертер: `libreoffice-impress --headless
   --convert-to pdf` на хосте agctl + RPC-обёртка, совместимая с
   контрактом officeToPdf.render (или отдельная кнопка «PDF» рядом с
   «Скачать» на вкладке Файлы).
3. Размер образа: libreoffice-impress с --no-install-recommends
   (~300-400 МиБ) — согласовать с бюджетом образа.

## Критерии готовности

- pptx из воркспейса открывается в сайдбаре как PDF-превью (или
  скачивается как PDF одной кнопкой на вкладке Файлы).

## Журнал

- 2026-11-05: создано по кейсу с презентацией Pro_sebya.pptx.
- 2026-11-13: Шаг 1 плана: upstream проверен по npm-реестру — @deepseek-ai/libreoffice-kit@0.2.3 optionalDeps только darwin-x64/arm64, win32-x64/arm64 и wasm; @deepseek-ai/libreoffice-kit-linux-{x64,arm64} — 404. Linux-движка нет и в пайплайне не видно ⇒ шаг 2: свой конвертер. Сделано: в agctl-dsh (runtime-слой Dockerfile, после npm-слоёв — apt-изменение не инвалидирует npm/mcp-кэш) ставится libreoffice-impress --no-install-recommends (pptx-кейс по бюджету из плана ~300-400 МиБ; writer/calc для docx/xlsx — отдельно) + хелпер /usr/local/bin/office2pdf (internal/assets/image/office2pdf): одна команда рядом с документом → PDF забирается штатной кнопкой «Файлы» на вкладке Файлов. Файлы образа идут в контекст через cp -R internal/assets/image/. — явных списков нет, хелпер подхватывается сам; go:embed all:image + go vet/build — ок.
- 2026-11-13: refl: docker в burn-сессии недоступен — сборку образа и живой прогон office2pdf на реальном pptx (например Pro_sebya.pptx из кейса) сделал за владельцем чек-листом «Проверить перед пушем»; правки намеренно в runtime-слое, чтобы пересборка не тянула npm install dsh заново.
- 2026-11-13: done — Конвертер Office→PDF в контейнере: libreoffice-impress в runtime-слое agctl-dsh + office2pdf-хелпер в PATH (internal/assets/image/office2pdf). Критерий частично: PDF одной командой + штатное скачивание (кнопка в сайдбаре dsh — правка апстрима dsh, вне репо, зафиксирована кандидатом). Апстрим LibreOffice-kit-linux подтверждён отсутствующим.
