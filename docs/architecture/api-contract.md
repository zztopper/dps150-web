# API contract: backend ↔ frontend (v1)

Зафиксировано для параллельной разработки F-005 (backend) и F-006 (frontend).
Изменения контракта — только через правку этого файла в том же MR.

Общие правила: JSON, camelCase, единицы — вольты/амперы/ватты/°C/Ач/Втч,
временные метки — unix millis (number). Ошибки:
`{"error": {"code": "<machine_code>", "message": "<human text>"}}` +
адекватный HTTP-статус (400 валидация, 409 устройство офлайн, 500 прочее).

## REST

### GET /healthz
`200 {"status":"ok"}` — liveness, без побочных эффектов.

### GET /api/v1/device
Текущее состояние устройства (из кэша hub'а, не ждёт устройство):

```json
{
  "connected": true,
  "transport": "tcp://10.20.0.5:2150",
  "info": {"model": "DPS-150", "hardware": "V1.0", "firmware": "V1.1"},
  "state": {
    "outputOn": false,
    "mode": "cv",
    "protection": "ok",
    "setpoints": {"voltage": 12.0, "current": 1.0},
    "measured": {"voltage": 11.99, "current": 0.5, "power": 6.0},
    "inputVoltage": 20.0,
    "temperature": 31.5,
    "limits": {"maxVoltage": 19.8, "maxCurrent": 5.1},
    "metering": {"capacityAh": 0.0, "energyWh": 0.0},
    "protections": {"ovp": 31.0, "ocp": 5.2, "opp": 155.0, "otp": 75.0, "lvp": 4.5},
    "brightness": 10,
    "volume": 5,
    "updatedAt": 1784000000000
  }
}
```

- `mode`: `"cc" | "cv"`; `protection`: `"ok" | "ovp" | "ocp" | "opp" | "otp" | "lvp" | "rep"`.
- `info` и `state` могут быть `null`, пока устройство ни разу не отвечало
  (`connected: false` при этом обязателен).

### PUT /api/v1/device/setpoints
Тело: `{"voltage": 12.0}` / `{"current": 0.5}` / оба поля. Значения
валидируются против `limits` (fallback 30.0 В / 5.0 А): вне диапазона —
`400 invalid_setpoint`. Устройство офлайн — `409 device_offline`.
Ответ `200`: `{"voltage": 12.0, "current": 0.5}` — применённые уставки.

### PUT /api/v1/device/output
Тело: `{"on": true}` или `{"on": false}`. Подтверждение включения — забота
UI, API дополнительных полей не требует. Офлайн — `409 device_offline`.
Ответ `200`: `{"on": true}`.

Зарезервировано на Этап 2 (НЕ реализуется в F-005/F-006):
`PUT /api/v1/device/protections`, `GET/POST /api/v1/profiles`,
`GET /api/v1/history`, `GET /api/v1/events`.

## WebSocket: GET /api/v1/ws

Только server→client. Формат сообщения: `{"type": "...", "data": {...}}`.

| type | когда | data |
|---|---|---|
| `state` | сразу после подключения клиента; после каждой успешной записи уставок/выхода | полный объект как в `GET /api/v1/device` |
| `telemetry` | каждый телеметрический пакет устройства (~2 Гц) | см. ниже |
| `status` | при смене связи с устройством | `{"connected": false, "transport": "..."}` |
| `event` | сработка защиты, смена CC/CV, вкл/выкл выхода | `{"kind": "protectionTrip"\|"modeChange"\|"outputChange", "protection": "ovp", "mode": "cc", "outputOn": true, "ts": 1784000000000}` — поля по смыслу kind |

`telemetry.data`:

```json
{
  "measured": {"voltage": 11.99, "current": 0.5, "power": 6.0},
  "inputVoltage": 20.0,
  "temperature": 31.5,
  "mode": "cv",
  "protection": "ok",
  "outputOn": true,
  "metering": {"capacityAh": 0.001, "energyWh": 0.02},
  "ts": 1784000000000
}
```

Клиент обязан переживать обрывы WS (реконнект с бэкоффом) и после
реконнекта заново строить состояние из первого `state`-сообщения.
Неизвестные `type` клиент молча игнорирует (forward-compat).

---

# API contract v2: Этап 2 (MVP)

Зафиксировано для параллельной разработки F-010..F-017 + TD-001.
Общие правила v1 действуют (camelCase, unix millis, формат ошибок).
Новый код ошибки: `storage_unavailable` (503) — БД недоступна (fail-soft).

## Профили (F-010)

`Profile`: `{"id": 1, "name": "3.3V logic", "voltage": 3.3, "current": 0.5,
"protections": {"ovp": 3.6, "ocp": 0.6, "opp": 10.0, "otp": 75.0, "lvp": 4.5},
"createdAt": <ms>, "updatedAt": <ms>}`

- `GET /api/v1/profiles` → `{"items": [Profile...]}` (сортировка по name)
- `POST /api/v1/profiles` (без id) → `201 Profile`; имя уникально → `409 profile_name_taken`
- `PUT /api/v1/profiles/{id}` → `200 Profile`; `404 profile_not_found`
- `DELETE /api/v1/profiles/{id}` → `204`
- `POST /api/v1/profiles/{id}/apply` → `200 {"applied": true}` — пишет в
  устройство C1/C2 + D1–D5 и подтверждает чтением. ИНВАРИАНТ: выход НЕ
  включается и НЕ выключается. Офлайн → `409 device_offline`.

## Аппаратные пресеты M1–M6 (F-011)

- `GET /api/v1/device/presets` → `{"items": [{"slot": 1, "voltage": 5.0, "current": 1.0}, ...]}`
  (6 слотов, из кэша FF-дампа)
- `PUT /api/v1/device/presets/{slot}` тело `{"profileId": 1}` ИЛИ
  `{"voltage": 5.0, "current": 1.0}` → `200 {"slot": 1, "voltage": 5.0, "current": 1.0}`.
  В ячейку уходят только V+I (железо не хранит защиты в пресетах).
  slot вне 1..6 → `400 invalid_slot`.

## Уставки защит (F-014)

- `PUT /api/v1/device/protections` тело `{"ovp"?: 31.0, "ocp"?: 5.2, "opp"?: 155.0,
  "otp"?: 75.0, "lvp"?: 4.5}` (любое подмножество) → `200` с применёнными
  значениями всех пяти. Валидация: > 0, ovp ≤ 31, ocp ≤ 5.2, opp ≤ 155,
  otp ≤ 80, lvp ≥ 0 → иначе `400 invalid_protection`. Проводной формат
  устройства — float32: значения, непредставимые конечным float32
  (> ~3.4e38), отклоняются тем же `400 invalid_protection`.

## История (F-012)

- `GET /api/v1/history?from=<ms>&to=<ms>&resolution=raw|1m|auto` →
  ```json
  {"resolution": "raw", "items": [
    {"ts": 1784..., "voltage": 12.0, "current": 0.5, "power": 6.0,
     "temperature": 31.5, "outputOn": true}
  ]}
  ```
  Для `1m` вместо мгновенных значений — `{"ts", "voltage": {"min","avg","max"},
  "current": {...}, "power": {...}, "temperature": {"avg"}, "samples": 120}`.
  `auto`: raw при (to-from) ≤ 2 ч, иначе 1m. from ≥ to или диапазон > 400 дней →
  `400 invalid_range`. Ответ ограничен 20000 точками (`400 range_too_dense` c
  подсказкой перейти на 1m). БД недоступна → `503 storage_unavailable`.

## Журнал событий (F-014, пишут все)

`Event`: `{"id": 1, "ts": <ms>, "kind": "...", "data": {...}}`; kinds:
`protectionTrip {protection, snapshot{voltage,current,power}}`,
`deviceConnected {}`, `deviceDisconnected {}`,
`outputOn {}`, `outputOff {}`,
`profileApplied {profileId, name}`,
`protectionsChanged {ovp,ocp,opp,otp,lvp}`,
`meteringSession {capacityAh, energyWh, durationMs}` (итог при выключении выхода),
`autoStop` (зарезервирован, Этап 3).

- `GET /api/v1/events?from&to&kind&limit=50&offset=0` →
  `{"items": [Event...], "total": 123}` (новые сверху; kind — CSV-фильтр)

## Настройки уведомлений (F-015)

- `GET /api/v1/settings/notifications` / `PUT ...` тело/ответ:
  `{"telegramEnabled": true, "events": {"protectionTrip": true,
  "deviceLink": true, "output": false, "meteringSession": true}}`
  Токен бота и chat id — ТОЛЬКО из env (`DPS_TELEGRAM_TOKEN`,
  `DPS_TELEGRAM_CHAT_ID`; в k8s — из VaultStaticSecret `secret/apps/dps150-web/telegram`),
  через API не читаются и не пишутся. Если env пусты — `telegramEnabled`
  игнорируется, `GET` дополнительно отдаёт `"configured": false`.

## WS-дополнения

Журнальные kinds, не имеющие v1-эквивалента на WS — `protectionsChanged`,
`profileApplied`, `meteringSession` — транслируются сообщением `event` (v1):
`data` = поля журнальной записи + `kind` + `ts`. Переходы линка и выхода
остаются v1-сообщениями `status` и `event` kind `outputChange`; журнальные
имена `deviceConnected`/`deviceDisconnected`/`outputOn`/`outputOff` на WS не
дублируются. `telemetry.data.metering` уже в v1. Новых типов сообщений нет.

## Схема БД (все времена unix millis, переносимый SQL)

- `profiles(id PK, name UNIQUE, voltage, current, ovp, ocp, opp, otp, lvp, created_at, updated_at)`
- `samples(ts BIGINT PK, voltage, current, power, input_voltage, temperature, output_on, mode, protection)` — 2 Гц, ретенция 30 дней
- `samples_1m(ts BIGINT PK, v_min, v_avg, v_max, i_min, i_avg, i_max, p_min, p_avg, p_max, t_avg, cnt)` — ретенция 365 дней
- `events(id PK autoincr, ts BIGINT INDEX, kind TEXT INDEX, data TEXT/JSON)`
- `settings(key PK, value)` — уже существует (F-007)

Ретенция и минутная агрегация — фоновые джобы бэкенда (интервалы — константы,
janitor раз в час). Модели подключаются через storage.Config.Models (F-007).

## Метрики (TD-001)

`GET /metrics` — Prometheus (promhttp), без Authelia-обхода не нуждается
(скрейпится изнутри кластера через ServiceMonitor на Service backend).

## Файловая структура фронтенда (чтобы параллельные треки не конфликтовали)

Роутер и каркас страниц закладываются в базовой ветке (react-router-dom,
AntD Layout с меню). Страницы: `src/pages/DashboardPage.tsx` (существующий
dashboard), `HistoryPage.tsx` (F-013), `ProfilesPage.tsx` (F-010/011),
`EventsPage.tsx` (F-014), `SettingsPage.tsx` (F-015). Dashboard-слоты —
отдельные файлы-компоненты: `src/components/LiveChart.tsx` (F-013),
`src/components/ProtectionsPanel.tsx` (F-014), `src/components/QuickProfiles.tsx`
(F-010), `src/components/MeteringCard.tsx` (F-017). Каждый трек добавляет ТОЛЬКО
свои файлы + одну строку подключения слота в DashboardPage (место помечено
комментариями-якорями `{/* slot:... */}`). i18n-ключи — с префиксом страницы.

---

# API contract v3: Этап 3 (v1.0)

Зафиксировано для параллельной разработки F-018/F-019/F-020. Правила v1/v2
действуют. Аутентификация — ADR-006: браузерный UI за Authelia (хост
`dps150.example.com`), скриптовый доступ — Bearer-токен на отдельном хосте
`dps150-api.example.com`. Backend-middleware на `/api/*`: пропускает запрос,
если есть валидный `Authorization: Bearer <token>` (с нужным scope) ИЛИ
доверенный заголовок `Remote-User` от Authelia; иначе 401 `unauthorized`.
Мутации (PUT/POST/DELETE) требуют scope `control`; GET — `read` или выше.

Гейт включается флагом `DPS_AUTH_REQUIRED` (env, default `false`). Локальный
однопользовательский запуск, docker-compose, e2e и mock работают с открытым
API (auth off); в кластере чарт выставляет `DPS_AUTH_REQUIRED=true`.

## Правила автостопа (F-018)

`AutomationRule`:
```json
{
  "id": 1, "name": "Заряд АКБ до отсечки", "enabled": true,
  "condition": {"type": "currentBelow", "amps": 0.05, "forSeconds": 300},
  "action": "outputOff",
  "scope": "session",
  "createdAt": <ms>, "updatedAt": <ms>,
  "lastTriggeredAt": <ms|null>
}
```
- `condition.type`: `currentBelow {amps, forSeconds}` | `capacityAbove {ah}` |
  `energyAbove {wh}` | `elapsedAbove {seconds}`. Длительность/гистерезис —
  на стороне движка (единичный выброс не срабатывает).
- `action`: пока только `outputOff` (зарезервировано на расширение).
- `scope`: `session` (действует только в рамках текущей сессии выхода, сбрасывается при выключении) | `always`.
- Эндпоинты: `GET /api/v1/automation/rules` → `{"items":[...]}`;
  `POST`/`PUT /api/v1/automation/rules/{id}`/`DELETE ...` (CRUD, 404 `rule_not_found`);
  `GET /api/v1/automation/triggers?limit&offset` → история срабатываний
  `{"items":[{"id","ruleId","ruleName","ts","reason"}],"total"}`.
- При срабатывании: выключить выход + событие журнала `autoStop
  {ruleId, ruleName, reason}` + Telegram. При потере связи с устройством
  правило переходит в `suspended` (не оценивается, срабатывания не копятся);
  движок исполняется в кластере — при обрыве автостоп НЕ гарантирован
  (подстраховка — аппаратные защиты). Состояние правила в WS-сообщении
  `event` kind `autoStop` при срабатывании.
- Хранение: таблица `automation_rules(id PK, name, enabled, condition JSON,
  action, scope, created_at, updated_at, last_triggered_at)`;
  `automation_triggers(id PK, rule_id INDEX, rule_name, ts INDEX, reason)`.

## Экспорт CSV (F-019)

- `GET /api/v1/history.csv?from&to&resolution` — стриминговый text/csv,
  `Content-Disposition: attachment; filename="dps150-history-<from>-<to>.csv"`.
  Колонки raw: `timestamp,voltage,current,power,temperature,output_on`
  (timestamp — ISO 8601 UTC). Для 1m: `timestamp,v_min,v_avg,v_max,i_min,
  i_avg,i_max,p_min,p_avg,p_max,t_avg,samples`. Ограничения диапазона — как
  у `/history` (invalid_range), но без лимита 20000 (стриминг). 503 при
  недоступной БД.
- `GET /api/v1/events.csv?from&to&kind` — стриминговый, колонки
  `timestamp,kind,data` (data — JSON-строка).
- Стриминг: строки пишутся по мере чтения из БД (курсор/пагинация), без
  сборки всего в память.

## API-токены (F-020)

`ApiToken` (метаданные, без секрета): `{"id":1, "name":"lab script",
"scope":"control", "createdAt":<ms>, "lastUsedAt":<ms|null>}`.
- `GET /api/v1/tokens` → `{"items":[ApiToken...]}` (только метаданные).
- `POST /api/v1/tokens` тело `{"name":"...", "scope":"read"|"control"}` →
  `201 {"token":"dps_<base64url>", ...ApiToken}` — СЕКРЕТ показывается
  единожды; в БД хранится только SHA-256 хэш.
- `DELETE /api/v1/tokens/{id}` → `204`; отозванный токен перестаёт
  работать немедленно (проверка хэша по БД, без кэша дольше запроса).
- Управление токенами доступно ТОЛЬКО через UI за Authelia (не по токену).
- Токены НИКОГДА не логируются (ни секрет, ни заголовок Authorization).

## Файловая структура фронтенда (Этап 3)

Новые страницы/компоненты в своих файлах: `src/pages/AutomationPage.tsx`
(F-018, маршрут /automation + пункт меню), кнопки экспорта на HistoryPage и
EventsPage (F-019), секция токенов в SettingsPage (F-020). i18n-префиксы:
`automation.*`, `export.*`, `tokens.*`. TD-002: консолидация history/events
типов из components/chart/* в `src/api/`.
