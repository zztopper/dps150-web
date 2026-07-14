# Git Workflow — «почти-TBD» (Trunk-Based Development)

Используется упрощённая модель TBD: одна главная ветка (`master`/`main`),
короткоживущие ветки фич, релизы — через теги. Никаких долгоживущих
`develop` / `release-*` веток.

## Карта веток и окружений

```
                  ┌─────────────────────────────────────────────┐
   feature/F-042  │  ─O─O─►   ephemeral dev env (review app)     │
                  └─────────┬───────────────────────────────────┘
                            │ MR (squash merge или fast-forward)
                            ▼
                  ┌─────────────────────────────────────────────┐
        master    │  ════════►   STAGING (preprod), auto-deploy │
                  └─────────┬───────────────────────────────────┘
                            │ git tag v1.4.0  (semantic version)
                            ▼
                  ┌─────────────────────────────────────────────┐
        v1.4.0    │  ════════►   PRODUCTION, deploy on tag      │
                  └─────────────────────────────────────────────┘

   hotfix/B-095   ─O─►  MR в master (срочно), затем тегом → prod
```

| Ветка / тег        | Окружение         | Trigger деплоя                                      |
|--------------------|-------------------|------------------------------------------------------|
| `master` / `main`  | **staging**       | Автоматический деплой на каждый push                 |
| `feature/<id>-<x>` | **review-app/dev**| Эфемерное окружение на push, удаляется при close MR  |
| `hotfix/<id>`      | **review-app/dev**| Аналогично feature, но быстрая дорожка к проду       |
| `vX.Y.Z` (tag)     | **production**    | Деплой только на push semver-тега                    |

## Правила

### 1. Master всегда зелёный
- Master/main всегда в состоянии, готовом к staging-деплою.
- CI на master обязан проходить полностью. Если pipeline красный —
  пайплайн `master`-деплоя блокируется до фикса (revert или forward-fix).

### 2. Короткоживущие feature-ветки
- Имя: `feature/F-042-export-pdf` (префикс Issue + короткое имя).
- Для багов: `bugfix/B-095-pedigree-edges`.
- Для техдолга: `tech-debt/TD-091-cleanup-helpers`.
- Время жизни: **≤ 3 дня**. Если дольше — декомпозировать Issue.
- Один MR ≈ один Issue. Не сваливаем несколько Issues в одну ветку.

### 3. Только MR/PR в master
- Прямые push в master запрещены (configure protected branch).
- Минимум одна аппрува: после прохождения **обязательной DA-верификации**
  и фиксов всех её находок (см. `docs/agents-team-workflow.md`).
- Squash merge по умолчанию. Fast-forward — для серий мелких commit'ов
  с осмысленной историей.

### 4. Релизы — через теги
- Семантическая версия: `vMAJOR.MINOR.PATCH`.
- Тег создаётся **из master** после ревью CHANGELOG'а и smoke-проверки на staging.
- Production деплоится только из тегов. Никаких ручных деплоев master в prod.
- Тег создаётся одной командой: `git tag -a vX.Y.Z -m "Release vX.Y.Z"`.

### 5. Hotfix
- Срочный bugfix в проде → ветка `hotfix/B-NNN-...` от **последнего prod-тега**,
  не от master.
- MR в master + cherry-pick в `master` (или forward-merge), затем новый patch-tag
  `vX.Y.(Z+1)` и деплой в prod.
- Параллельно открыть Issue с `bug`-label и линком на hotfix MR.

### 6. Branch protection rules (обязательны)

Master/main защищён на стороне сервера:

**GitLab (Settings → Repository → Protected branches):**
- `master`/`main`: Push — `No one`, Merge — `Maintainers`.
- Required pipeline statuses: `lint`, `test`, `build`, `e2e`, `security:scan`,
  `lint:swagger`, `lint:changelog`, `lint:likec4`, `commitlint`.
- Required approvals: ≥ 1 (CODEOWNERS auto-assign).
- «Require all threads to be resolved» — включено.

**GitHub (Settings → Branches → Branch protection rules):**
- `master`/`main`: «Require pull request reviews», CODEOWNERS as required reviewers.
- «Require status checks» — все обязательные jobs.
- «Require linear history» — да (squash или rebase merge).
- «Do not allow bypassing» — да.

Без этих правил весь pre-commit gate бесполезен: можно push прямо в master.

### 7. Conventional Commits — обязательно

Формат: `<type>(<scope>): <subject>`. Типы: `feat`, `fix`, `refactor`, `perf`,
`test`, `docs`, `build`, `ci`, `chore`, `revert`.

Примеры:
- `feat(api): add /export endpoint for PDF`
- `fix(auth): correct token expiry calculation`
- `refactor(repo): extract user lookup helper`

CI-job `commitlint` валидирует формат на каждом MR. Локально —
`commitlint.config.js` (см. `templates/commitlint.config.js`).
Без правильного формата merge запрещён.

### 8. Эфемерные dev-окружения
- Каждая `feature/*` ветка автоматически получает временное окружение
  (review app / preview deployment).
- URL окружения публикуется в MR коммент-ом из CI.
- Окружение **удаляется автоматически** при close/merge MR.
- Используется для:
  - QA-агенту прогонять E2E-сценарии.
  - System Analyst показывать stakeholder'у работающую фичу.
  - Devil's Advocate проверять edge cases в браузере.

## CI/CD маппинг

```yaml
# Концептуально (адаптировать под GitLab CI / GitHub Actions / Argo CD)

deploy:dev:
  rules:
    - if: $CI_COMMIT_BRANCH =~ /^(feature|bugfix|tech-debt|hotfix)\//
  environment:
    name: review/$CI_COMMIT_REF_SLUG
    url: https://$CI_COMMIT_REF_SLUG.dev.example.com
    on_stop: stop:dev

deploy:staging:
  rules:
    - if: $CI_COMMIT_BRANCH == "master"
  environment:
    name: staging
    url: https://staging.example.com

deploy:production:
  rules:
    - if: $CI_COMMIT_TAG =~ /^v\d+\.\d+\.\d+$/
  environment:
    name: production
    url: https://example.com
```

## Что НЕ делаем

- ❌ Долгоживущие ветки `develop`, `release-1.x`, `staging`.
  В TBD они не нужны: master всегда production-ready, staging — это **окружение**, а не ветка.
- ❌ Разделение «фича-ветки» и «релиз-ветки» по типу gitflow.
- ❌ Накопление 50 коммитов в feature-ветке. Если фича большая — это значит,
  декомпозиция в Issue провалена; разбейте на несколько коротких MR.
- ❌ Hotfix через прямой push в master.

## Чек-лист перед мерджем feature-ветки

- [ ] Issue имеет статус In Progress, исполнитель — вы.
- [ ] CI зелёный (lint, test, build).
- [ ] Devil's Advocate провёл верификацию, все CRITICAL/HIGH устранены,
      MEDIUM/LOW/WISHES обработаны или явно отклонены с обоснованием.
- [ ] CHANGELOG.md обновлён (`[Unreleased]`).
- [ ] При архитектурном решении — ADR в Wiki опубликован (см. `docs/adr-workflow.md`).
- [ ] Описание MR содержит:
  - `Closes #NNN` (или `Closes !NNN`).
  - Краткое summary.
  - Скриншоты для UI или curl-примеры для API.
- [ ] После мерджа: feature-ветка удалена, эфемерное dev-окружение убрано
      автоматически.
