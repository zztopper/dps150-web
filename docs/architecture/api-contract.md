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
