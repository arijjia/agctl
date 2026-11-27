---
schema: 1.0.0
tags: [go, k8s, llm-gateway]
cli-std: 1.0.0
cli:
  - name: agctl
    entry: dist/agctl
    install: make install link
    completions: completions/agctl.fish
  - name: agctl-server
    entry: dist/agctl-server
    install: make install link
    completions: completions/agctl-server.fish
---
Манифест репо для gitctl (формат — карточка `repo-manifest`, backlog
osx-git). agctl — Go-платформа LLM-шлюза/агентов с Kubernetes-раннером:
cmd/agctl, internal/, e2e-скрипты в bin/ (e2e-*.sh, capacity-bench),
профили и dsh-плагины. Бинари собираются в dist/; `make install` ставит их в ~/.agctl/bin
(раскладка релиза), `make link` — относительные симлинки ~/.local/bin и
fish-комплишены на completions/ репо; метки инвазивности в комплишенах и
справке сверяет тест с каноном меток (cli-std 1.0.0; 1.1.0 — табличный вывод
list-команд, ещё нет). Стейт-плит launchd нет; локального stейта не держит
(контейнеры/kind). Манифест заведён патрулём тика 20261115-0200
(new-repo-sector-patrol).
