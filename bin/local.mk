# ── local: локальный режим (agctl dev) из исходников, по дому на профиль ────
# Логика — bin/local.sh. Всё под корнем $(AGCTL_ROOT) (~/.agctl): бинарь
# bin/agctl, профиль profiles/<id> (ссылка на сиблинг ../agctl-profile-<id>),
# дом homes/<id>. Профиль задаётся явно в командной строке
# (make local_up PROFILE=<id>); без него — нейтральный agctl (дом homes/agctl,
# порт 8930). Порт дома организации выбирается один раз и живёт в его
# config.yaml. Бинарь не качается из релиза: local_up сначала делает make install.

LOCAL_PROFILE := $(if $(filter command line,$(origin PROFILE)),$(PROFILE),agctl)
LOCAL_SH      := bin/local.sh

.PHONY: local_up local_init local_down local_status local_paths local_logs local_open local_doctor local_img local_list

## local_up: make install + init (если нужно) + agctl dev up для PROFILE (по умолчанию agctl); LOCAL_WORKDIR — папка при первом init
local_up: install
	@$(LOCAL_SH) up $(LOCAL_PROFILE)

## local_init: настроить дом профиля без запуска (профиль-сиблинг, порт, LOCAL_WORKDIR)
local_init:
	@$(LOCAL_SH) init $(LOCAL_PROFILE)

## local_down / local_status / local_logs / local_open / local_doctor: agctl dev … в доме PROFILE
local_down:
	@$(LOCAL_SH) down $(LOCAL_PROFILE)
local_status:
	@$(LOCAL_SH) status $(LOCAL_PROFILE)
## local_paths: где что лежит у дома PROFILE (agctl dev paths): корень, профиль, дом, порты, контейнеры, маунты, секреты
local_paths:
	@$(LOCAL_SH) paths $(LOCAL_PROFILE)
local_logs:
	@$(LOCAL_SH) logs $(LOCAL_PROFILE)
local_open:
	@$(LOCAL_SH) open $(LOCAL_PROFILE)
local_doctor:
	@$(LOCAL_SH) doctor $(LOCAL_PROFILE)

## local_img: пересобрать образы (img_build с надстройкой PROFILE) и перевести рабочее место на них (agctl dev update)
local_img:
	@$(LOCAL_SH) init $(LOCAL_PROFILE) >/dev/null
	@$(MAKE) --no-print-directory img_build PROFILE=$(LOCAL_PROFILE)
	@$(LOCAL_SH) update $(LOCAL_PROFILE)

## local_list: все дома локального режима на машине: профиль, адрес, состояние
local_list:
	@$(LOCAL_SH) list
