# agctl поверх Google AX и Agent Substrate

Файлы этого каталога — ресурсы Substrate/AX, которые agctl не создаёт сам:

| Файл | Что |
|---|---|
| `workerpool.yaml` | тёплые gVisor-воркеры для песочниц dsh (ko apply из checkout Substrate) |
| `egress-policy.example.yaml` | инъекция ключа LLM egress-шлюзом вместо env задачи |

Сам шлюз agctl в режиме `ax` — `deploy/k8s/overlays/ax`. Порядок установки,
маппинг понятий (пользователь → Task → актор), ограничения режима и смоук —
`docs/kubernetes.md`.
