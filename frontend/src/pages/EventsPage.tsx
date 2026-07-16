import { useState } from 'react'
import {
  Alert,
  Button,
  Card,
  DatePicker,
  Empty,
  Flex,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import dayjs, { type Dayjs } from 'dayjs'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
import type { TFunction } from 'i18next'
import { ApiError } from '../api/client'
import { JOURNAL_KINDS, type JournalEvent } from '../api/events'
import { eventsCsvUrl, triggerDownload } from '../api/export'
import { useEventsLiveInvalidation, useEventsQuery } from '../hooks/useEvents'

// dayjs is a transitive dependency of antd's DatePicker (see
// HistoryPage.tsx) — only used here to seed a default export range.
const { RangePicker } = DatePicker
const EXPORT_DEFAULT_SPAN_MS = 24 * 60 * 60 * 1000

const PAGE_SIZE = 20

function formatTimestamp(ts: number, locale: string): string {
  return new Date(ts).toLocaleString(locale, {
    dateStyle: 'short',
    timeStyle: 'medium',
  })
}

function num(data: Record<string, unknown>, key: string): number | undefined {
  const v = data[key]
  return typeof v === 'number' ? v : undefined
}

function str(data: Record<string, unknown>, key: string): string | undefined {
  const v = data[key]
  return typeof v === 'string' ? v : undefined
}

/** One-line human summary of a journal entry's `data`, per kind. */
function summarizeEvent(event: JournalEvent, t: TFunction): string {
  const d = event.data ?? {}
  switch (event.kind) {
    case 'protectionTrip': {
      const protection = str(d, 'protection')
      const snapshotRaw = d.snapshot
      const snapshot =
        typeof snapshotRaw === 'object' && snapshotRaw !== null
          ? (snapshotRaw as Record<string, unknown>)
          : {}
      if (protection === undefined) {
        return ''
      }
      return t('events.summary.protectionTrip', {
        protection: protection.toUpperCase(),
        voltage: num(snapshot, 'voltage')?.toFixed(2) ?? '—',
        current: num(snapshot, 'current')?.toFixed(3) ?? '—',
        power: num(snapshot, 'power')?.toFixed(2) ?? '—',
      })
    }
    case 'profileApplied': {
      const name = str(d, 'name')
      return name !== undefined ? t('events.summary.profileApplied', { name }) : ''
    }
    case 'protectionsChanged': {
      return (['ovp', 'ocp', 'opp', 'otp', 'lvp'] as const)
        .map((key) => {
          const v = num(d, key)
          return v !== undefined ? `${key.toUpperCase()} ${v}` : null
        })
        .filter((part): part is string => part !== null)
        .join(' · ')
    }
    case 'meteringSession': {
      const durationMs = num(d, 'durationMs')
      return t('events.summary.meteringSession', {
        capacity: num(d, 'capacityAh')?.toFixed(3) ?? '—',
        energy: num(d, 'energyWh')?.toFixed(3) ?? '—',
        durationMin: durationMs !== undefined ? (durationMs / 60000).toFixed(1) : '—',
      })
    }
    default:
      return ''
  }
}

/**
 * Event journal (F-014): paginated GET /api/v1/events with a kind
 * filter, localized timestamps, an expandable raw-JSON row, and live
 * updates via WS `event`/`status` messages invalidating the query.
 */
export function EventsPage() {
  const { t, i18n } = useTranslation()
  useEventsLiveInvalidation()

  const [searchParams] = useSearchParams()
  const [page, setPage] = useState(1)
  // Seed the filter from a marker deep-link (?kind=…, possibly repeated).
  // The events table has no from/to filter, so those params are ignored.
  const [kindFilter, setKindFilter] = useState<string[]>(() =>
    searchParams.getAll('kind'),
  )
  // Export range (F-019): the journal table itself is unbounded (no
  // from/to), but the CSV export backend requires one — defaults to the
  // last 24 h, adjustable independently of the on-screen kind filter/
  // pagination above.
  const [exportRange, setExportRange] = useState<[Dayjs, Dayjs]>(() => {
    const to = Date.now()
    return [dayjs(to - EXPORT_DEFAULT_SPAN_MS), dayjs(to)]
  })

  function selectExportRange(values: [Dayjs | null, Dayjs | null] | null) {
    const [from, to] = values ?? [null, null]
    if (from === null || to === null) {
      return
    }
    setExportRange([from, to])
  }

  function handleExport() {
    triggerDownload(
      eventsCsvUrl(exportRange[0].valueOf(), exportRange[1].valueOf(), kindFilter),
    )
  }

  const query = useEventsQuery({
    kinds: kindFilter.length > 0 ? kindFilter : undefined,
    limit: PAGE_SIZE,
    offset: (page - 1) * PAGE_SIZE,
  })

  const storageUnavailable =
    query.error instanceof ApiError && query.error.code === 'storage_unavailable'

  const kindOptions = JOURNAL_KINDS.map((kind) => ({
    value: kind,
    label: t(`events.kinds.${kind}`),
  }))

  const columns: ColumnsType<JournalEvent> = [
    {
      title: t('events.table.time'),
      dataIndex: 'ts',
      key: 'ts',
      width: 210,
      render: (ts: number) => (
        <span className="tabular">{formatTimestamp(ts, i18n.language)}</span>
      ),
    },
    {
      title: t('events.table.kind'),
      dataIndex: 'kind',
      key: 'kind',
      width: 220,
      render: (kind: string) => <Tag>{t(`events.kinds.${kind}`, kind)}</Tag>,
    },
    {
      title: t('events.table.summary'),
      key: 'summary',
      render: (_, event) => summarizeEvent(event, t),
    },
  ]

  return (
    <Flex vertical gap="middle">
      <Typography.Title level={4} style={{ margin: 0 }}>
        {t('events.title')}
      </Typography.Title>

      {storageUnavailable && (
        <Alert
          type="error"
          showIcon
          title={t('events.errors.storageUnavailableTitle')}
          description={t('events.errors.storageUnavailable')}
          action={
            <Button size="small" onClick={() => void query.refetch()}>
              {t('common.retry')}
            </Button>
          }
        />
      )}

      <Card size="small">
        <Flex wrap gap="middle" align="center" justify="space-between">
          <Space size="middle" wrap>
            <Typography.Text type="secondary">{t('export.range')}</Typography.Text>
            <RangePicker
              showTime
              value={exportRange}
              onChange={selectExportRange}
              placeholder={[t('export.customFrom'), t('export.customTo')]}
              aria-label={t('export.range')}
              className="dps-range-picker"
              classNames={{ popup: { root: 'dps-range-popup' } }}
            />
          </Space>
          <Button onClick={handleExport}>{t('export.button')}</Button>
        </Flex>
      </Card>

      <Card>
        <Flex vertical gap="middle">
          <Select
            mode="multiple"
            allowClear
            optionFilterProp="label"
            // Only 9 kinds total: virtualizing the dropdown buys nothing
            // and its list-recycling caused stale/hidden option nodes.
            virtual={false}
            aria-label={t('events.filter.placeholder')}
            placeholder={t('events.filter.placeholder')}
            style={{ minWidth: 280 }}
            options={kindOptions}
            value={kindFilter}
            onChange={(value: string[]) => {
              setKindFilter(value)
              setPage(1)
            }}
          />
          <Table<JournalEvent>
            rowKey="id"
            columns={columns}
            dataSource={query.data?.items ?? []}
            loading={query.isFetching}
            pagination={{
              current: page,
              pageSize: PAGE_SIZE,
              total: query.data?.total ?? 0,
              onChange: setPage,
              showTotal: (total) => t('events.pagination.total', { total }),
            }}
            expandable={{
              expandedRowRender: (event) => (
                <pre style={{ margin: 0, overflowX: 'auto' }}>
                  {JSON.stringify(event.data, null, 2)}
                </pre>
              ),
              rowExpandable: (event) => Object.keys(event.data ?? {}).length > 0,
            }}
            scroll={{ x: 'max-content' }}
            locale={{ emptyText: <Empty description={t('events.empty')} /> }}
          />
        </Flex>
      </Card>
    </Flex>
  )
}
