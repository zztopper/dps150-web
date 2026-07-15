# Runbook: ser2net on pve (DPS-150 → cluster bridge)

Status: **deployed and verified live 2026-07-14** (I-001).

## Topology

The FNIRSI DPS-150 is connected over USB to the Proxmox host `pve` (10.20.0.5) and shows up as
`/dev/serial/by-id/usb-Artery_AT32_Virtual_Com_Port_XXXXXXXX-if00`
(→ `/dev/ttyACM0`). ser2net exposes the port as raw TCP `10.20.0.5:2150`;
the backend connects with `DPS_TRANSPORT=tcp://10.20.0.5:2150`.

> `XXXXXXXX` — replace with your device's serial number: `ls /dev/serial/by-id/`.

## Installation (done)

```bash
apt-get install -y ser2net        # Debian 13, ser2net 4.6.4
cp /etc/ser2net.yaml /etc/ser2net.yaml.orig
cat > /etc/ser2net.yaml <<'EOF'
%YAML 1.1
---
# dps150-web: FNIRSI DPS-150 serial-over-TCP bridge (runbook: docs/runbooks/ser2net-pve.md)

connection: &dps150
  accepter: tcp,2150
  # replace XXXXXXXX with your device's serial number: ls /dev/serial/by-id/
  connector: serialdev,/dev/serial/by-id/usb-Artery_AT32_Virtual_Com_Port_XXXXXXXX-if00,115200n81,local
  options:
    kickolduser: true
    max-connections: 1
EOF
systemctl enable --now ser2net
```

The firewall was intentionally left unconfigured (owner's decision); port 2150 is reachable
from the entire local network.

## Key decisions

- **by-id path** instead of /dev/ttyACM0 — stable across USB reconnects
  and when other devices appear.
- **`kickolduser: true`**: the only legitimate client is the backend hub.
  A new connection evicts the old one, so a crashed/hung hub
  (half-open TCP) does not block the port until the keepalive timeout. With
  `kickolduser: false`, a stuck client occupies the single slot
  (`max-connections: 1`) and all new connections are silently closed —
  which is exactly what the first failed installation looked like.

## ⚠️ DPS-150 hardware quirk (not in the protocol doc!)

The device **silently drops commands sent back-to-back**.
A GetAll sent immediately after SessionEnable/SetBaud never gets a
reply; with a pause of ≥50 ms between writes it always answers (reproducibly
verified on firmware V1.2). The original dps150tool does a
`usleep(50000)` after every command for the same reason.

The backend accounts for this by pacing writes in the hub (`WithWriteGap`, 50 ms
by default). When writing any other clients — keep the pause.

## Verification

```bash
# 1. Service is up, port is listening
systemctl is-active ser2net && ss -tlnp | grep 2150

# 2. Live protocol test (telemetry starts after session enable):
python3 - <<'EOF'
import socket, time
s = socket.create_connection(('10.20.0.5', 2150), timeout=5)
s.sendall(bytes.fromhex('F1C100010102'))  # session enable
s.settimeout(3)
print('telemetry:', s.recv(256).hex() or 'NOTHING')
s.close()
EOF

# 3. Full backend test (locally):
DPS_TRANSPORT=tcp://10.20.0.5:2150 ./dps150-server &
curl -s localhost:8080/api/v1/device | jq .connected   # → true
```

## Recovery

- USB re-plugged / device changed its number: the by-id path does not change,
  ser2net reopens the port itself on the next client connection.
- Port "busy", clients drop immediately: `systemctl restart ser2net`
  (clears the hung session) and check that nobody else is holding a
  TCP connection to :2150.
- After replacing the PSU itself, the serial number in by-id changes — update the path
  in `/etc/ser2net.yaml` and `systemctl restart ser2net`.
