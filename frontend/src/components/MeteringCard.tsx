import { useEffect, useRef, useState } from 'react'
import { Card, Flex, Typography } from 'antd'
import { useTranslation } from 'react-i18next'
import { useDevice } from '../state/useDevice'
import { fmtAh, fmtDuration, fmtWh } from './meteringFormat'
import '../styles/metering.css'

/**
 * Live metering (F-017) — Dashboard slot `slot:metering-card`.
 *
 * `telemetry.metering` (capacityAh/energyWh) accumulates continuously
 * while the device keeps metering enabled — it does NOT reset when the
 * output cycles off/on (see backend/internal/notify/metering.go). This
 * card therefore shows two things side by side: the raw running counters,
 * and the current session's elapsed time (since the output last turned
 * on). The card is muted while the output is off and prominent while it
 * is on. Below that, the summary of the last *finished* session — as
 * journaled by the backend under kind `meteringSession` — is shown; it
 * is fetched once via REST and kept live over the WS `event` passthrough
 * (see docs/architecture/api-contract.md, "WS-дополнения").
 */

interface LastSession {
  capacityAh: number
  energyWh: number
  durationMs: number
}

/** Shape of the REST `GET /events?kind=meteringSession` item's `data`. */
interface MeteringSessionJournalData {
  capacityAh?: unknown
  energyWh?: unknown
  durationMs?: unknown
}

interface EventsPageResponse {
  items?: Array<{ kind?: unknown; data?: MeteringSessionJournalData }>
}

function isFiniteNumber(v: unknown): v is number {
  return typeof v === 'number' && Number.isFinite(v)
}

function parseSessionData(data: unknown): LastSession | null {
  if (typeof data !== 'object' || data === null) {
    return null
  }
  const d = data as MeteringSessionJournalData
  if (
    !isFiniteNumber(d.capacityAh) ||
    !isFiniteNumber(d.energyWh) ||
    !isFiniteNumber(d.durationMs)
  ) {
    return null
  }
  return { capacityAh: d.capacityAh, energyWh: d.energyWh, durationMs: d.durationMs }
}

/**
 * The WS `event` passthrough of a journal entry flattens kind/ts and the
 * journal payload into one object (see api/ws.go `updateMessage`), unlike
 * the REST `GET /events` item where the payload sits under `data`. The
 * shared `EventData` type (api/types.ts) only models the v1 event kinds,
 * so a meteringSession message is narrowed here at runtime instead of
 * widening the shared type.
 */
function parseMeteringSessionWsEvent(event: unknown): LastSession | null {
  if (typeof event !== 'object' || event === null) {
    return null
  }
  const e = event as { kind?: unknown } & MeteringSessionJournalData
  if (e.kind !== 'meteringSession') {
    return null
  }
  return parseSessionData(e)
}

/** Fetches the most recent finished metering session, if any. */
async function fetchLastSession(): Promise<LastSession | null> {
  const resp = await fetch('/api/v1/events?kind=meteringSession&limit=1')
  if (!resp.ok) {
    return null
  }
  const body = (await resp.json()) as EventsPageResponse
  const item = body.items?.[0]
  if (item === undefined || item.kind !== 'meteringSession') {
    return null
  }
  return parseSessionData(item.data)
}

export function MeteringCard() {
  const { t } = useTranslation()
  const { state, lastEvent } = useDevice()

  const outputOn = state?.outputOn ?? false
  const capacityAh = state?.metering.capacityAh ?? 0
  const energyWh = state?.metering.energyWh ?? 0

  // Track the moment the output last turned on (client-side timestamp,
  // taken at the point the transition is observed — WS latency is
  // negligible for a duration display).
  const [sessionStart, setSessionStart] = useState<number | null>(null)
  const prevOutputOn = useRef<boolean | null>(null)
  useEffect(() => {
    if (outputOn && prevOutputOn.current !== true) {
      setSessionStart(Date.now())
    } else if (!outputOn) {
      setSessionStart(null)
    }
    prevOutputOn.current = outputOn
  }, [outputOn])

  // Live-ticking elapsed time while a session is open.
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (sessionStart === null) {
      return
    }
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [sessionStart])

  const elapsedMs = sessionStart === null ? 0 : now - sessionStart

  // Last finished session: seeded via REST, refreshed live over WS.
  const [lastSession, setLastSession] = useState<LastSession | null>(null)
  const [lastSessionLoaded, setLastSessionLoaded] = useState(false)
  useEffect(() => {
    let cancelled = false
    fetchLastSession()
      .then((session) => {
        if (!cancelled) {
          setLastSession(session)
          setLastSessionLoaded(true)
        }
      })
      .catch(() => {
        if (!cancelled) {
          setLastSessionLoaded(true)
        }
      })
    return () => {
      cancelled = true
    }
  }, [])
  useEffect(() => {
    const session = parseMeteringSessionWsEvent(lastEvent)
    if (session !== null) {
      setLastSession(session)
    }
  }, [lastEvent])

  return (
    <Card
      title={t('metering.title')}
      className={outputOn ? 'metering-card metering-card-active' : 'metering-card metering-card-idle'}
    >
      <Flex vertical gap="middle">
        <Flex wrap gap="large">
          <div className="metering-stat">
            <Typography.Text type="secondary">{t('metering.capacity')}</Typography.Text>
            <div className="metering-stat-value tabular">
              {fmtAh(capacityAh)} <span className="reading-unit">{t('units.ampHour')}</span>
            </div>
          </div>
          <div className="metering-stat">
            <Typography.Text type="secondary">{t('metering.energy')}</Typography.Text>
            <div className="metering-stat-value tabular">
              {fmtWh(energyWh)} <span className="reading-unit">{t('units.wattHour')}</span>
            </div>
          </div>
          <div className="metering-stat">
            <Typography.Text type="secondary">{t('metering.sessionDuration')}</Typography.Text>
            <div className="metering-stat-value tabular">
              {outputOn ? fmtDuration(elapsedMs) : t('metering.outputOff')}
            </div>
          </div>
        </Flex>
        <div>
          <Typography.Text type="secondary">{t('metering.lastSession')}</Typography.Text>
          <div className="tabular">
            {!lastSessionLoaded
              ? '…'
              : lastSession === null
                ? t('metering.noLastSession')
                : t('metering.lastSessionSummary', {
                    ah: fmtAh(lastSession.capacityAh),
                    wh: fmtWh(lastSession.energyWh),
                    duration: fmtDuration(lastSession.durationMs),
                  })}
          </div>
        </div>
      </Flex>
    </Card>
  )
}
