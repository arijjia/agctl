---
schema: 1.0.0
tags: [go, k8s, llm-gateway]
---
Манифест репо для gitctl (формат — карточка `repo-manifest`, backlog
osx-git). agctl — Go-платформа LLM-шлюза/агентов с Kubernetes-раннером:
cmd/agctl, internal/, e2e-скрипты в bin/ (e2e-*.sh, capacity-bench),
профили и dsh-плагины. Бинарь собирается в dist/ (не в реестре cli-std:
нет симлинка ~/.local/bin и fish-комплишенов — когда станет CLI парка,
завести по cli-std). Стейт-плит launchd нет; локального stейта не держит
(контейнеры/kind). Манифест заведён патрулём тика 20261115-0200
(new-repo-sector-patrol).
