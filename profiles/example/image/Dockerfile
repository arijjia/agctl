# ─────────────────────────────────────────────────────────────────────────────
# image/Dockerfile — надстройка образа рабочего места организации.
#
# Что это:   слой поверх базового образа agctl-dsh. Сюда ставится то, что
#            должно быть у каждого сотрудника: CLI, сертификаты, зеркала.
# Кто собирает: `make img_profile PROFILE=example` (из исходников) или agctl
#            при первом запуске, если готового образа нет. Результат —
#            agctl-dsh-example:<тег>.
# Подключение: profile.yaml → images.dockerfile: image/Dockerfile.
# Правило:   BASE задаёт agctl; не меняйте ENTRYPOINT и пользователя.
# ─────────────────────────────────────────────────────────────────────────────
ARG BASE
FROM ${BASE}

# Корпоративный корневой сертификат (положите файл рядом и раскомментируйте):
# COPY corp-root-ca.crt /usr/local/share/ca-certificates/corp-root-ca.crt
# RUN update-ca-certificates

# Инструменты команды — одной строкой, с очисткой кэша пакетов:
RUN apt-get update \
 && apt-get install -y --no-install-recommends make postgresql-client \
 && rm -rf /var/lib/apt/lists/*
