# Runbook: ser2net на pve (мост DPS-150 → кластер)

Статус: **установлено и проверено вживую 2026-07-14** (I-001).

## Схема

FNIRSI DPS-150 подключён по USB к Proxmox-хосту `pve` (10.20.0.5) и виден как
`/dev/serial/by-id/usb-Artery_AT32_Virtual_Com_Port_XXXXXXXX-if00`
(→ `/dev/ttyACM0`). ser2net публикует порт как raw TCP `10.20.0.5:2150`;
бэкенд подключается с `DPS_TRANSPORT=tcp://10.20.0.5:2150`.

> `XXXXXXXX` — замените на серийник вашего устройства: `ls /dev/serial/by-id/`.

## Установка (выполнено)

```bash
apt-get install -y ser2net        # Debian 13, ser2net 4.6.4
cp /etc/ser2net.yaml /etc/ser2net.yaml.orig
cat > /etc/ser2net.yaml <<'EOF'
%YAML 1.1
---
# dps150-web: FNIRSI DPS-150 serial-over-TCP bridge (runbook: docs/runbooks/ser2net-pve.md)

connection: &dps150
  accepter: tcp,2150
  # замените XXXXXXXX на серийник вашего устройства: ls /dev/serial/by-id/
  connector: serialdev,/dev/serial/by-id/usb-Artery_AT32_Virtual_Com_Port_XXXXXXXX-if00,115200n81,local
  options:
    kickolduser: true
    max-connections: 1
EOF
systemctl enable --now ser2net
```

Файрвол сознательно не настраивался (решение владельца); порт 2150 доступен
всей локальной сети.

## Ключевые решения

- **by-id путь** вместо /dev/ttyACM0 — стабилен при переподключении USB
  и появлении других устройств.
- **`kickolduser: true`**: единственный легитимный клиент — hub бэкенда.
  Новое подключение вытесняет старое, поэтому упавший/зависший hub
  (half-open TCP) не блокирует порт до keepalive-таймаута. С
  `kickolduser: false` подвисший клиент занимает единственный слот
  (`max-connections: 1`) и все новые подключения молча закрываются —
  именно так выглядела первая неудачная установка.

## ⚠️ Аппаратная особенность DPS-150 (нет в протокол-доке!)

Устройство **молча теряет команды, отправленные вплотную друг к другу**.
GetAll, отправленный сразу после SessionEnable/SetBaud, никогда не получает
ответа; с паузой ≥50 мс между записями — отвечает всегда (проверено
воспроизводимо на firmware V1.2). Оригинальный dps150tool делает
`usleep(50000)` после каждой команды по той же причине.

В бэкенде это учтено пейсингом записей в hub (`WithWriteGap`, 50 мс
по умолчанию). При написании любых других клиентов — держите паузу.

## Проверка

```bash
# 1. Сервис жив, порт слушается
systemctl is-active ser2net && ss -tlnp | grep 2150

# 2. Живой протокольный тест (телеметрия пойдёт после session enable):
python3 - <<'EOF'
import socket, time
s = socket.create_connection(('10.20.0.5', 2150), timeout=5)
s.sendall(bytes.fromhex('F1C100010102'))  # session enable
s.settimeout(3)
print('telemetry:', s.recv(256).hex() or 'NOTHING')
s.close()
EOF

# 3. Полный тест бэкендом (локально):
DPS_TRANSPORT=tcp://10.20.0.5:2150 ./dps150-server &
curl -s localhost:8080/api/v1/device | jq .connected   # → true
```

## Восстановление

- USB передёрнули / устройство сменило номер: by-id путь не меняется,
  ser2net сам переоткрывает порт при следующем подключении клиента.
- Порт «занят», клиенты отваливаются сразу: `systemctl restart ser2net`
  (снимает подвисшую сессию) и проверить, что никто другой не держит
  TCP-подключение к :2150.
- После замены самого БП поменяется серийник в by-id — обновить путь
  в `/etc/ser2net.yaml` и `systemctl restart ser2net`.
