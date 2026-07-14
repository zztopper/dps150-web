# MCP-серверы

Boilerplate комплектуется набором MCP-серверов из коробки в `.mcp.json`.
Они дополняют Claude Code навыками семантического поиска по коду,
документацией библиотек, persistent memory, браузерной автоматизацией и
доступом к API трекеров.

JSON в `.mcp.json` — **проектный** scope: каждый разработчик/агент в этом
проекте получает один и тот же набор серверов. Личные глобальные настройки
живут в `~/.claude/` (мы их не трогаем).

## Состав по умолчанию

| Сервер              | Назначение                                                  | Включён |
|---------------------|-------------------------------------------------------------|---------|
| `serena`            | Семантический поиск/редактирование кода (LSP-aware)         | да      |
| `context7`          | Актуальная документация библиотек/SDK/CLI                   | да      |
| `memory`            | Persistent memory между сессиями (knowledge graph)          | да      |
| `playwright`        | Браузерная автоматизация и E2E-тесты                        | да      |
| `github`            | Прямой доступ к GitHub API (Issues, PRs, code search)       | условно |
| `gitlab`            | Прямой доступ к GitLab API (Issues, MRs)                    | условно |

«Условно» = включён в `.mcp.json`, но падает без токена. Заполните
`GITHUB_TOKEN` и/или `GITLAB_TOKEN` в `.env`, либо удалите соответствующую
секцию из `.mcp.json`, если не используете провайдера.

> ⚠️ **Исключён по соображениям безопасности:** `sequential-thinking` MCP.
> Для цепочек рассуждений используем встроенное extended thinking Claude 4 —
> оно нативное, без внешних зависимостей.

## Предусловия

| Зависимость       | Зачем                                                                | macOS                       | Linux                          |
|-------------------|----------------------------------------------------------------------|-----------------------------|--------------------------------|
| `node` ≥ 20       | npx-сервера                                                          | `brew install node`         | `nvm install 20`               |
| `python` 3.13     | serena (через uvx)                                                   | `brew install python@3.13`  | `apt install python3.13` или pyenv |
| `uvx` (uv)        | Запуск Serena без глобальной установки                               | `brew install uv`           | `pipx install uv`              |
| `docker`          | Опциональный fallback для memory как контейнера                      | `brew install --cask docker`| `apt install docker.io`        |

Проверка: `node --version && python3.13 --version && uvx --version`.

## Краткое описание серверов

### serena
- **Что даёт**: `find_symbol`, `find_references`, `get_symbols_overview`,
  `rename_symbol`, `safe_delete_symbol` — это LSP-aware навигация по коду.
- **Почему стоит**: на больших репах быстрее `grep`, понимает типы.
- **Конфиг**: `.serena/project.yml` — перечень языков (`go`, `python`, `typescript`,
  `rust`, …).
- **Память**: `.serena/memories/` — заметки о проекте, которые Serena подкладывает
  в контекст. Создавайте/редактируйте через её же tool `write_memory`.

### context7
- **Что даёт**: всегда-свежая документация для библиотек, SDK, CLI, cloud-сервисов.
- **Когда полезно**: при обращении к API/сетапу любой библиотеки — даже знакомых.
- **Настройка ключа**: hosted версия Upstash работает без ключа для
  публичных пакетов; для приват-док нужен `CONTEXT7_API_KEY` в env (опц.).

### memory
- **Что даёт**: knowledge graph (entities/relations/observations), который
  переживает сессии Claude Code.
- **Где хранится**: `MEMORY_FILE_PATH` (по дефолту `.claude/memory/mcp-memory.jsonl`).
- **Не путать** с auto-memory `MEMORY.md`: они дополняют друг друга:
  - `MEMORY.md` — индексированные заметки в формате markdown, читаются Claude напрямую;
  - `mcp-memory.jsonl` — структурированный граф, доступ через MCP-tools.
- Если не используете — удалите секцию `memory` из `.mcp.json`.

### playwright
- **Что даёт**: запуск браузера, навигация, скриншоты, evaluate JS, click/type/fill.
- **Когда полезно**: E2E-тесты, проверка UI вживую перед merge,
  auto-screenshot для PR.
- **Зависимость**: при первом запуске скачает браузер (~150 МБ).

### github
- **Что даёт**: чтение/создание Issues, PRs, code search, просмотр файлов
  через API без локального git.
- **Токен**: `GITHUB_TOKEN` (или `GITHUB_PERSONAL_ACCESS_TOKEN`) — fine-grained
  с правами Issues / Pull requests / Contents (read+write).

### gitlab
- **Что даёт**: чтение/создание Issues, MRs, Wiki-страниц, code search.
- **Токен**: `GITLAB_TOKEN` с правами `api`, `read_repository`, `write_repository`.
- **GITLAB_HOST_URL**: для self-hosted указать `https://gitlab.example.com/api/v4`.
- **Альтернатива**: разные коммьюнити-серверы (`@yoda.digital/gitlab-mcp-server`,
  `@modelcontextprotocol/server-gitlab` и др.). По умолчанию указан yoda.digital,
  как наиболее живой fork — поменяйте на свой при необходимости.

## Активация

После заполнения `.env`:

1. `claude` (Claude Code) при старте автоматически подхватывает `.mcp.json`
   из корня проекта.
2. Первый запуск каждого MCP-сервера может занимать секунды-минуты
   (npm-кэш, скачивание pip-пакета для serena, скачивание браузера для playwright).
3. Проверка статуса:
   ```
   /mcp
   ```
   Внутри Claude Code — список активных MCP с состоянием.

## Включение/отключение конкретного сервера

Удалите соответствующий ключ из `.mcp.json` или закомментируйте
(JSON не поддерживает комментарии — храните «отключённую» версию рядом
как `.mcp.full.json` для документации).

Альтернатива — глобальные настройки `~/.claude/mcp.json`. Не делайте
проектные сервера глобальными: разные проекты потребуют разные токены.

## Безопасность

- ❌ **Никогда** не вписывайте токены прямо в `.mcp.json`.
- ✅ Используйте `${ENV_VAR}` в секции `env` — Claude Code подставит
  значение из shell environment во время запуска MCP-сервера.
- ✅ `.env` добавлен в `.gitignore`. Перед `claude` в shell делайте
  `source .env && export $(grep -v '^#' .env | xargs)` или используйте
  `direnv` / любой подобный инструмент.

## Troubleshooting

| Симптом                                  | Причина / фикс                                                                            |
|------------------------------------------|--------------------------------------------------------------------------------------------|
| `serena` стартует 30+ секунд              | первая `uvx` сборка; ускорьте `uv cache prune` и предкомпилируйте `uvx --from ... serena --help` |
| `playwright` падает на `chromium not found` | запустите `npx playwright install` один раз                                              |
| `github` MCP возвращает 401              | токен невалиден или scope недостаточен — пересоздать                                        |
| `gitlab` MCP не видит Issues              | проверьте `GITLAB_HOST_URL` (полный URL до `/api/v4`)                                      |
| `memory` не сохраняет между сессиями      | убедитесь что `MEMORY_FILE_PATH` указывает на постоянный путь (не `/tmp/...`)             |
| Claude Code «не видит» MCP-инструменты    | в чате выполните `/mcp` и `/mcp restart <server>`                                          |
