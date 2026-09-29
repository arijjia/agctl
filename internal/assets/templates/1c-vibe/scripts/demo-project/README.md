# Демо-репозиторий 1c-vibe (контент для GitLab)

Синтетическое дерево «выгрузки конфигурации 1С» — для демонстрации цикла
«задача в Jira → правка агентом → MR → проверка CI». XML упрощён по сравнению
с реальной выгрузкой платформы, но структура каталогов — как в настоящей:
`Configuration.xml`, `Catalogs/<Имя>/Object.xml`, `Catalogs/<Имя>/Ext/*.bsl`.

Содержимое этого каталога кладётся в отдельный git-проект (см. `../README.md`).

## Локальная проверка линтером (без GitLab CI)

```
curl -sSL -o /tmp/bsl-ls.jar https://github.com/1c-syntax/bsl-language-server/releases/latest/download/bsl-language-server.jar
java -jar /tmp/bsl-ls.jar analyze --srcDir . --format json --outputFile /tmp/lint.json
```

Требуется Java 11+. Флаги сверяй с `java -jar bsl-ls.jar analyze --help`
(формат менялся между версиями).

## CI

`.gitlab-ci.yml` запускает BSL Language Server в headless-режиме и валит
пайплайн при диагностике уровня Error (`ci/check_lint.py`). Требования к
раннеру: docker-образ, доступ в интернет (apt + github releases). Если раннер
без интернета — jar можно положить в проект и убрать curl из job.
