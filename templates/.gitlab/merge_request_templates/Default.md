<!--
Имя MR в Conventional Commits: feat(scope): краткое описание
                              fix(scope): ...
                              refactor(scope): ...
                              docs(scope): ...
-->

## Summary
<!-- 2-4 предложения о том, что и зачем сделано. -->

Closes #NNN

## Changes
- ...
- ...

## Test plan
- [ ] Unit
- [ ] Integration
- [ ] E2E
- [ ] Coverage report attached

## Screenshots / API examples
<!-- UI: до/после; API: curl/httpie с примером запроса и ответа. -->

## Architecture / Data flow
- [ ] LikeC4 обновлён, если затронута архитектура / новый флоу
- [ ] `npx likec4 validate` зелёный
- [ ] ADR опубликован в Wiki, если принято архитектурное решение
- [ ] Ссылка на ADR: ...

## Migration / rollout
- [ ] Миграция БД expand-contract (если применимо), см. `docs/migration-safety.md`
- [ ] Feature flag (default OFF) для рискованной фичи
- [ ] Rollback план: ...

## Devil's Advocate report
<!-- Вставить summary DA-отчёта или ссылку. -->
- Verdict: [ ] APPROVED [ ] BLOCKED
- Tracked Issues для не-устранённых находок: ...

## DoD checklist (`docs/definition-of-done.md`)
- [ ] Реализация: AC выполнены
- [ ] Тесты: пирамида зелёная (unit ~60 %, integration, E2E)
- [ ] Качество: lint + type-check + SAST/SCA зелёные
- [ ] Архитектура: ADR + LikeC4
- [ ] Документация: CHANGELOG, README, Swagger
- [ ] Operations: миграции
- [ ] Security: threat model, нет секретов, RBAC
- [ ] DA: APPROVED, все находки tracked
- [ ] Pre-commit gate: `./scripts/precommit-check.sh` зелёный
- [ ] Git: Conventional Commits, ветка ≤ 3 дня

/label ~needs-review
