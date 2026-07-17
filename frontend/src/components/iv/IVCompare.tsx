import { useMemo, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Checkbox,
  Empty,
  Flex,
  Segmented,
  Space,
  Spin,
  Table,
  Tooltip,
  Typography,
  theme,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { CloseOutlined, DownloadOutlined } from '@ant-design/icons'
import { useQueries } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
import type { TFunction } from 'i18next'
import { getIVSweep, type IVSweep } from '../../api/iv'
import { triggerTextDownload } from '../../api/export'
import { modeFromBg, overlaySeriesColors } from '../chart/colors'
import { IVCompareChart, type IVCompareSeries } from './IVCompareChart'
import { metricUnitSuffix } from './ivFormat'
import {
  allSameComponent,
  buildCompareCsv,
  compareMetricRows,
  parseCompareIds,
  serializeCompareIds,
  type CompareCell,
  type CompareMetricRow,
} from './ivCompareUtils'

/** A stable, human label for a sweep in the overlay/CSV/legend. */
function sweepLabel(s: IVSweep): string {
  return `${s.profileName} #${s.id}`
}

/** Renders one comparison-metric cell null-safe: value+unit, ≈ for approx, else "—". */
function metricCell(t: TFunction, cell: CompareCell, row: CompareMetricRow) {
  if (!cell.available || cell.value === null) {
    return (
      <Typography.Text type="secondary" aria-label={t('iv.metrics.notDetermined')}>
        —
      </Typography.Text>
    )
  }
  const text = `${row.format(cell.value)}${metricUnitSuffix(t, row.unit)}`
  return cell.approx ? (
    <Tooltip title={t('iv.metrics.approxHint')}>
      <span className="tabular">≈ {text}</span>
    </Tooltip>
  ) : (
    <span className="tabular">{text}</span>
  )
}

/** Renders an aggregate (min/max/spread) cell: value+unit, or "—" when < 2 samples. */
function aggregateCell(t: TFunction, value: number | null, row: CompareMetricRow) {
  if (value === null) {
    return (
      <Typography.Text type="secondary" aria-label={t('iv.metrics.notDetermined')}>
        —
      </Typography.Text>
    )
  }
  return <span className="tabular">{`${row.format(value)}${metricUnitSuffix(t, row.unit)}`}</span>
}

/**
 * The Сравнение tab (F-025 / ADR-010): a client-side overlay + metrics comparison
 * of recorded sweeps, driven entirely by the `?ids=` URL param (the single,
 * bookmarkable source of truth). The loader dedupes/validates the ids and takes
 * the first 8 distinct valid ones (`parseCompareIds`), each resolved with the
 * existing `GET /iv/sweeps/{id}`; a stale/deleted id 404s and is skipped with a
 * note. There is NO backend comparison route.
 */
export function IVCompare() {
  const { t, i18n } = useTranslation()
  const { token } = theme.useToken()
  const [searchParams, setSearchParams] = useSearchParams()
  const [logY, setLogY] = useState(false)
  // Ephemeral per-curve visibility (the URL holds the SELECTION; hiding a curve is
  // transient exploration). Missing key ⇒ visible.
  const [hidden, setHidden] = useState<Record<number, boolean>>({})

  const parsed = useMemo(() => parseCompareIds(searchParams.get('ids')), [searchParams])
  const ids = parsed.ids

  // Resolve each selected id with the authoritative single-sweep read. A shared
  // query key with the History detail drawer means an already-open sweep is warm.
  const results = useQueries({
    queries: ids.map((id) => ({
      queryKey: ['iv', 'sweep', id],
      queryFn: () => getIVSweep(id),
    })),
  })

  const anyLoading = results.some((r) => r.isLoading)
  // `results` are fresh objects each render but their `.data` is the stable cached
  // sweep, so recomputing inline is cheap and identity-safe for the derived views.
  const sweeps = ids
    .map((_, i) => results[i]?.data)
    .filter((s): s is IVSweep => s !== undefined)
  const missingCount = results.filter((r) => r.isError).length

  const palette = overlaySeriesColors(modeFromBg(token.colorBgContainer))
  const series: IVCompareSeries[] = sweeps.map((s, i) => ({
    id: s.id,
    label: sweepLabel(s),
    color: palette[i % palette.length],
    points: s.points,
    visible: !(hidden[s.id] ?? false),
  }))

  const shared = allSameComponent(sweeps)
  const metricRows = shared === null ? [] : compareMetricRows(sweeps, shared)

  const writeIds = (next: number[]) => {
    const params = new URLSearchParams(searchParams)
    if (next.length === 0) {
      params.delete('ids')
    } else {
      params.set('ids', serializeCompareIds(next))
    }
    // replace: the selection is a bookmarkable state, not a new history entry
    // (matching the ?range=/?tab= convention).
    setSearchParams(params, { replace: true })
  }

  const removeId = (id: number) => writeIds(ids.filter((x) => x !== id))
  const clearAll = () => writeIds([])
  const toggleVisible = (id: number, visible: boolean) =>
    setHidden((h) => ({ ...h, [id]: !visible }))

  const handleExportCsv = () => {
    const csv = buildCompareCsv(
      sweeps.map((s) => ({ sweepId: s.id, label: sweepLabel(s), points: s.points })),
    )
    triggerTextDownload('dps150-iv-compare.csv', csv)
  }

  // Loader note: invalid tokens, cap-8 truncation, and deleted/stale ids.
  const notes: string[] = []
  if (parsed.invalidCount > 0) {
    notes.push(t('iv.compare.noteInvalid', { count: parsed.invalidCount }))
  }
  if (parsed.truncated) {
    notes.push(t('iv.compare.noteTruncated', { max: 8 }))
  }
  if (missingCount > 0) {
    notes.push(t('iv.compare.noteMissing', { count: missingCount }))
  }

  if (ids.length === 0) {
    return (
      <Card>
        <Empty description={t('iv.compare.empty')}>
          <Typography.Text type="secondary">{t('iv.compare.emptyHint')}</Typography.Text>
        </Empty>
      </Card>
    )
  }

  // Remount the chart on scale / selection / theme / locale change (uPlot captures
  // these at creation); visibility is mutated live inside the chart.
  const chartKey = `${logY ? 'log' : 'lin'}-${series.map((s) => s.id).join('.')}-${token.colorBgContainer}-${i18n.language}`

  const sweepColumns: ColumnsType<CompareMetricRow> = sweeps.map((s, i) => ({
    title: (
      <Space size={4}>
        <span
          aria-hidden
          style={{
            display: 'inline-block',
            width: 10,
            height: 10,
            borderRadius: 2,
            background: palette[i % palette.length],
          }}
        />
        <span>{sweepLabel(s)}</span>
      </Space>
    ),
    key: `s-${s.id}`,
    render: (_: unknown, row: CompareMetricRow) => metricCell(t, row.cells[i], row),
  }))

  const columns: ColumnsType<CompareMetricRow> = [
    {
      title: t('iv.compare.metric'),
      key: 'metric',
      fixed: 'left',
      render: (_: unknown, row: CompareMetricRow) => t('iv.metrics.' + row.key),
    },
    ...sweepColumns,
    {
      title: t('iv.compare.min'),
      key: 'min',
      render: (_: unknown, row: CompareMetricRow) => aggregateCell(t, row.min, row),
    },
    {
      title: t('iv.compare.max'),
      key: 'max',
      render: (_: unknown, row: CompareMetricRow) => aggregateCell(t, row.max, row),
    },
    {
      title: t('iv.compare.spread'),
      key: 'spread',
      render: (_: unknown, row: CompareMetricRow) => aggregateCell(t, row.spread, row),
    },
  ]

  return (
    <Flex vertical gap="middle">
      <Flex align="center" justify="space-between" wrap gap="small">
        <Typography.Title level={5} style={{ margin: 0 }}>
          {t('iv.compare.title', { count: sweeps.length })}
        </Typography.Title>
        <Space wrap>
          <Segmented
            value={logY ? 'log' : 'linear'}
            onChange={(v) => setLogY(v === 'log')}
            aria-label={t('iv.chart.scale.label')}
            options={[
              { label: t('iv.chart.scale.linear'), value: 'linear' },
              { label: t('iv.chart.scale.log'), value: 'log' },
            ]}
          />
          <Button
            icon={<DownloadOutlined />}
            onClick={handleExportCsv}
            disabled={sweeps.length === 0}
          >
            {t('iv.compare.exportCsv')}
          </Button>
          <Button onClick={clearAll}>{t('iv.compare.clear')}</Button>
        </Space>
      </Flex>

      {notes.length > 0 && (
        <Alert type="info" showIcon message={notes.join(' · ')} />
      )}

      <Spin spinning={anyLoading && sweeps.length === 0}>
        {sweeps.length === 0 ? (
          <Card>
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              description={anyLoading ? t('iv.compare.loading') : t('iv.compare.allMissing')}
            />
          </Card>
        ) : (
          <Flex vertical gap="middle">
            <Card>
              <IVCompareChart key={chartKey} series={series} logY={logY} />
              {/* Legend: swatch + label + show/hide + remove — the cue is never
                  colour alone (accessibility: color-not-only). */}
              <Flex wrap gap="small" style={{ marginTop: 12 }}>
                {series.map((s) => (
                  <Space key={s.id} size={4} className="dps-iv-legend-item">
                    <Checkbox
                      checked={s.visible}
                      onChange={(e) => toggleVisible(s.id, e.target.checked)}
                    >
                      <Space size={6}>
                        <span
                          aria-hidden
                          style={{
                            display: 'inline-block',
                            width: 12,
                            height: 12,
                            borderRadius: 3,
                            background: s.color,
                            opacity: s.visible ? 1 : 0.3,
                          }}
                        />
                        <span style={{ textDecoration: s.visible ? 'none' : 'line-through' }}>
                          {s.label}
                        </span>
                      </Space>
                    </Checkbox>
                    <Button
                      type="text"
                      size="small"
                      icon={<CloseOutlined />}
                      aria-label={t('iv.compare.remove', { label: s.label })}
                      onClick={() => removeId(s.id)}
                    />
                  </Space>
                ))}
              </Flex>
            </Card>

            <Card
              title={t('iv.compare.metricsTitle')}
              styles={{ body: { paddingTop: 12 } }}
            >
              {shared === null ? (
                <Alert type="info" showIcon message={t('iv.compare.mixedTypes')} />
              ) : metricRows.length === 0 ? (
                <Empty
                  image={Empty.PRESENTED_IMAGE_SIMPLE}
                  description={t('iv.metrics.noneForComponent')}
                />
              ) : (
                <Table<CompareMetricRow>
                  rowKey="key"
                  size="small"
                  columns={columns}
                  dataSource={metricRows}
                  pagination={false}
                  scroll={{ x: 'max-content' }}
                />
              )}
            </Card>
          </Flex>
        )}
      </Spin>
    </Flex>
  )
}
