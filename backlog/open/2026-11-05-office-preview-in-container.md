---
schema: 1.0.0
id: office-preview-in-container
title: "Превью Office-файлов в контейнере: нет Linux-движка у libreoffice-kit"
status: open
priority: P3
size: M
area: server
repo: arijjia/agctl
created: 2026-11-05
updated: 2026-11-05
tags: [dsh, preview]
depends: []
epic: ""
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
