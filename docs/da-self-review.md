# Devil's Advocate: self-review boilerplate

DA-аудит самого boilerplate. **Все находки устранены в рамках boilerplate.**
У нового пользователя на старте проекта техдолга нет.

## CRITICAL — устранено

### [C1] Не было гейта на background agents и одиночные subagents
**Проблема:** правила «параллелизм через TeamCreate» носили рекомендательный
характер. Ничто не мешало агенту вызвать `Task` без `team_name` или с
`run_in_background=true`, обходя tmux-окно и контроль тимлида.
**Фикс:** ✅ `.claude/hooks/enforce-agents-team.sh` + `.claude/settings.json` с
`PreToolUse: Task`. Любой `Task` без `team_name` или с `run_in_background=true`
блокируется на уровне инструмента, ещё до вызова.

### [C2] sequential-thinking MCP включён по умолчанию (CVE-уязвимости)
**Проблема:** упомянутый сервер имел известные проблемы безопасности.
**Фикс:** ✅ удалён из `.mcp.json`, `docs/mcp-servers.md`, всех skill-файлов
и `CLAUDE.md`. Расчёт на extended thinking Claude 4 (нативный, без MCP).

### [C3] Нет Definition of Ready / Definition of Done
**Проблема:** «AC + RICE» в Issue недостаточно: непонятно, когда Issue готова
к старту и когда фактически закрыта. Размытые границы → reflux в DA-цикле.
**Фикс:** ✅ `docs/definition-of-ready.md`, `docs/definition-of-done.md`.
Каждое Issue теперь имеет два жёстких чек-листа.

### [C4] Нет ограничения на «найди → исправь → перепроверь»
**Проблема:** цикл DA-fix-DA может уйти в бесконечный пинг-понг. Пользователь
платит за токены параллелизма, но без ограничения это может дегенерировать.
**Фикс:** ✅ в `docs/agents-team-workflow.md` добавлены лимиты:
- Максимум **3 цикла** DA → fixes → DA в одной сессии без human-checkpoint.
- На 4-м цикле — обязательная пауза, эскалация пользователю с резюме оставшихся
  проблем, оценкой стоимости продолжения и предложением декомпозиции.

### [C5] Нет human checkpoint между этапами
**Проблема:** агентская команда могла полностью реализовать большую фичу без
ни одного промежуточного апрува, что плохо при ошибочной интерпретации ТЗ.
**Фикс:** ✅ обязательные точки человеческого согласия:
- После Architect/System Analyst и **до** старта реализации (план + ADR + LikeC4).
- После DA-Verdict APPROVED и **до** push в remote.
Описано в `docs/agents-team-workflow.md`.

## HIGH — устранено

### [H1] Нет миграционной безопасности
**Фикс:** ✅ `docs/migration-safety.md` — паттерн expand → migrate data →
contract; запрет на `ALTER TABLE` с длинной блокировкой; чек-лист DBA.

### [H2] Нет Issue / MR templates
**Фикс:** ✅ `templates/.gitlab/...`, `templates/.github/...` —
шаблоны для feature, bug, MR/PR, выровненные с DoR/DoD.

### [H3] Нет CODEOWNERS / branch protection описания
**Фикс:** ✅ `templates/CODEOWNERS.example` + раздел «Branch protection
rules» в `docs/git-workflow.md`.

### [H4] Conventional Commits не закреплены
**Фикс:** ✅ обязательный формат Conventional Commits, `commitlint.config.js`
+ опциональный pre-push hook, описание в `docs/git-workflow.md`.




### [H8] Нет release process / hotfix flow
**Фикс:** ✅ `docs/release-process.md` — semver, CHANGELOG → release notes,
hotfix-флоу с возвращением фикса в master, теги-кандидаты `vX.Y.Z-rc.N`.

### [H9] Memory: 3 источника (`MEMORY.md` / serena memories / MCP memory)
**Фикс:** ✅ раздел «Memory precedence» в `docs/superpowers-integration.md`
и `CLAUDE.md`: реальный код > MEMORY.md > serena > MCP memory.

### [H10] init-project.sh перетирал существующие файлы без подтверждения
**Фикс:** ✅ обновлён `init-project.sh`: автоматический backup в `.boilerplate-backup/`
с timestamp; `--force` флаг для подавления; явное логирование.

## MEDIUM — устранено



### [M3] Нет глоссария
**Фикс:** ✅ `templates/glossary.md.template`.



### [M6] Нет cost / FinOps в контексте AI-агентов
**Фикс:** ✅ `docs/token-budget.md` + `scripts/agents-cost-report.sh`.

## LOW — устранено

### [L1] init-project.sh: нет update-флоу
**Фикс:** ✅ `VERSION` + `CHANGELOG.md` + `scripts/boilerplate-update.sh`.

### [L2] Нет mutation testing / property-based testing
**Фикс:** ✅ Раздел «Tier-2 testing» в `docs/testing-pyramid.md`.

### [L3] Нет SBOM / license compliance в pre-commit
**Фикс:** ✅ `precommit-check.sh --full` запускает CycloneDX, license-checker,
gitleaks, trivy, govulncheck.

## Verdict

**APPROVED для использования.** У нового пользователя на старте проекта
техдолга нет: все процессы документированы, скрипты включены, hooks настроены.

Этот документ сохраняется в проекте как пример **финального DA-отчёта** —
оркестратор может использовать его как образец для будущих фич.
