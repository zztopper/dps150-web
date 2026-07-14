# Release Process

Релизы — через **семантические теги** из master. Никакого ручного деплоя из ветки.

## Версионирование (SemVer 2.0)

`vMAJOR.MINOR.PATCH`:
- **MAJOR** — breaking changes API / схемы / поведения.
- **MINOR** — новая функциональность, обратно-совместимая.
- **PATCH** — bugfix, без изменения публичного контракта.

Pre-release: `v1.2.0-rc.1`, `v1.2.0-beta.2`. Деплой только в staging.

## CHANGELOG

`CHANGELOG.md` ведём в стиле **Keep a Changelog**:

```markdown
# Changelog

## [Unreleased]
### Added
- ...
### Changed
- ...
### Deprecated
- ...
### Removed
- ...
### Fixed
- ...
### Security
- ...

## [1.2.0] - 2026-04-15
...
```

Каждый MR обязан добавлять строку в `[Unreleased]`. Без этого
`make lint:changelog` (обязательная CI-проверка) красный — блок merge.

## Cut a release

```bash
# 1. Убедиться, что master зелёный и staging-deploy прошёл smoke.
glab pipeline view --status success
gh run list --branch master --status success

# 2. Прогнать DA-проверку release-кандидата (`docs/da-self-review.md` как образец).

# 3. Перенести [Unreleased] → [1.2.0] - <today>, оставить пустой [Unreleased].
$EDITOR CHANGELOG.md

# 4. Тег.
git tag -a v1.2.0 -m "Release v1.2.0"
git push origin v1.2.0

# 5. CI/CD задеплоит prod автоматически (см. docs/git-workflow.md).
```

## Release notes

Автогенерация из CHANGELOG + Conventional Commits:
- GitLab: Releases API, attach CHANGELOG-секцию.
- GitHub: Releases, generate from tag commit history.
- Включить:
  - Highlights (3–5 главных пунктов).
  - Breaking changes (если есть) с migration guide.
  - Deprecations с Sunset-датами.
  - Полный список через `git log v1.1.0..v1.2.0 --oneline`.

## Hotfix flow

1. Bug найден в проде → severity SEV1/SEV2.
2. Создать `hotfix/B-NNN-<short>` от **последнего prod-тега** (не master).
3. Минимальный фикс + regression-test, **не сваливать с другой работой**.
4. Полный pre-commit gate (включая E2E).
5. Devil's Advocate за 30 минут (может быть сжатый, но обязательный).
6. MR в master + merge.
7. Cherry-pick / forward-merge в master, если ветка отошла.
8. Новый patch-tag `v1.2.1`, push.
9. Prod-деплой, мониторинг.

## Release candidate

Для крупных MAJOR-релизов:
- Сначала `v2.0.0-rc.1` → деплой staging → 7 дней мониторинга.
- Если всё OK: `v2.0.0` → prod.
- Если проблемы: `v2.0.0-rc.2` с фиксами, новый цикл.

## Запреты

- ❌ Откатывать тег (`git tag -d` + push). Если v1.2.0 битая — выпускаем v1.2.1
  с фиксом, не «исправляем» v1.2.0.
- ❌ Деплой `master` в prod вручную. Только через тег.
- ❌ Skip changelog для «срочного» MR. Никогда не срочно настолько, чтобы
  сломать процесс.
- ❌ Удалять старые теги. История релизов — навсегда.

## Чек-лист перед `git tag`

- [ ] CI на master зелёный.
- [ ] Staging-deploy на этом коммите прошёл smoke + основные user flows.
- [ ] CHANGELOG `[Unreleased]` непуст и осмыслен.
- [ ] При наличии БД-миграций — план expand-contract выполнен (`docs/migration-safety.md`).
- [ ] Release notes собраны.
- [ ] Stakeholder уведомлён.
