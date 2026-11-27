---
schema: 1.0.0
tags: [go, k8s, llm-gateway]
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
fish-комплишены на completions/ репо (cli-std; метки инвазивности в
комплишенах и справке — карточка cli-std-markers). Стейт-плит launchd нет; локального stейта не держит
(контейнеры/kind). Манифест заведён патрулём тика 20261115-0200
(new-repo-sector-patrol).
