#!/usr/bin/env bash
# rice-template.sh — вывести шаблон описания Issue с RICE-таблицей.
#
# Использование:
#   ./scripts/rice-template.sh > /tmp/issue-body.md
#   ./scripts/rice-template.sh "Название фичи" > /tmp/issue-body.md
#
# Затем отредактируйте файл, попросите команду из 3 агентов
# (Architect, System Analyst, Devil's Advocate) проставить оценки,
# и передайте файл в issue-create.sh:
#   ./scripts/issue-create.sh F "Название фичи" feature,backend /tmp/issue-body.md

TITLE="${1:-Название фичи или задачи}"

cat <<EOF
# $TITLE

## Бизнес-контекст
<!-- Зачем это нужно. Какую боль решает. Для кого. -->

## Acceptance Criteria
<!-- Given/When/Then или простой чек-лист. -->
- [ ] AC-1: ...
- [ ] AC-2: ...

## Out of scope
- ...

## Технические детали
<!-- API-контракт, схема БД, миграции, интеграции. -->

## Риски и зависимости
- ...

---

## RICE-оценка

Команда из трёх агентов оценивает независимо. Финальный RICE = (avg_R × avg_I × avg_C) / avg_E.

| Голос              | Reach (1-5) | Impact (0.25-3.0) | Confidence (0-1) | Effort (PM) | RICE  |
|--------------------|------------:|------------------:|-----------------:|------------:|------:|
| Architect          |             |                   |                  |             |       |
| System Analyst     |             |                   |                  |             |       |
| Devil's Advocate   |             |                   |                  |             |       |
| **Среднее**        |             |                   |                  |             | **?** |

### Обоснование
- **Architect:** ...
- **System Analyst:** ...
- **Devil's Advocate:** ...

EOF
