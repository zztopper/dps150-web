import { useMemo, useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import {
  Alert,
  Button,
  Card,
  Checkbox,
  DatePicker,
  Empty,
  Flex,
  Segmented,
  Space,
  Spin,
  theme,
  Typography,
} from 'antd'
import type { Dayjs } from 'dayjs'
import { useTranslation } from 'react-i18next'
import { ApiError, fetchEvents, fetchHistory } from '../api/client'
import { historyCsvUrl, triggerDownload } from '../api/export'
import { HistoryChart, type VisibleSeries } from '../components/chart/HistoryChart'
import { mapHistoryToAlignedData } from '../components/chart/mapHistory'
import {
  RANGE_PRESETS,
  type MsRange,
  type RangePreset,
  presetRangeMs,
} from '../components/chart/rangePresets'

// dayjs is a transitive dependency of antd's DatePicker (no direct
// package.json entry needed — RangePicker's own value/onChange contract
// is dayjs objects; only the `Dayjs` type is used directly here).
const { RangePicker } = DatePicker

function apiErrorMessage(
  t: (key: string, options?: Record<string, unknown>) => string,
  err: unknown,
): string {
  if (err instanceof ApiError) {
    switch (err.code) {
      case 'range_too_dense':
        return t('history.error.rangeTooDense')
      case 'invalid_range':
        return t('history.error.invalidRange')
      case 'storage_unavailable':
        return t('history.error.storageUnavailable')
      default:
        return t('errors.requestFailed', { detail: err.message })
    }
  }
  return t('errors.network')
}

/** Measurement history over a time range (F-012/F-013). */
export function HistoryPage() {
  const { t, i18n } = useTranslation()
  const { token } = theme.useToken()

  const [preset, setPreset] = useState<RangePreset | 'custom'>('day')
  const [customRange, setCustomRange] = useState<[Dayjs, Dayjs] | null>(null)
  const [baseRange, setBaseRange] = useState<MsRange>(() =>
    presetRangeMs('day', Date.now()),
  )
  const [viewRange, setViewRange] = useState<MsRange>(baseRange)
  const [visibleSeries, setVisibleSeries] = useState<VisibleSeries>({
    voltage: true,
    current: true,
    power: true,
    temperature: true,
  })

  function selectPreset(next: RangePreset) {
    const range = presetRangeMs(next, Date.now())
    setPreset(next)
    setCustomRange(null)
    setBaseRange(range)
    setViewRange(range)
  }

  function selectCustomRange(values: [Dayjs | null, Dayjs | null] | null) {
    const [from, to] = values ?? [null, null]
    if (from === null || to === null) {
      return
    }
    const range = { from: from.valueOf(), to: to.valueOf() }
    setPreset('custom')
    setCustomRange([from, to])
    setBaseRange(range)
    setViewRange(range)
  }

  function handleZoom(fromMs: number, toMs: number) {
    if (toMs - fromMs < 1000) {
      return
    }
    setViewRange({ from: fromMs, to: toMs })
  }

  function handleResetZoom() {
    setViewRange(baseRange)
  }

  // Exports the currently viewed range (F-019): same [from, to] the
  // chart itself is querying, and the same `resolution=auto` request
  // `fetchHistory` sends above — the backend resolves it to raw/1m from
  // the span exactly as the on-screen query did.
  function handleExport() {
    triggerDownload(historyCsvUrl(viewRange.from, viewRange.to, 'auto'))
  }

  const isZoomed = viewRange.from !== baseRange.from || viewRange.to !== baseRange.to

  const historyQuery = useQuery({
    queryKey: ['chart-history', viewRange.from, viewRange.to],
    queryFn: () => fetchHistory(viewRange.from, viewRange.to, 'auto'),
    placeholderData: keepPreviousData,
    retry: false,
  })

  const eventsQuery = useQuery({
    queryKey: ['chart-events', viewRange.from, viewRange.to],
    queryFn: () => fetchEvents(viewRange.from, viewRange.to),
    placeholderData: keepPreviousData,
    retry: false,
  })

  const chartData = useMemo(
    () => (historyQuery.data ? mapHistoryToAlignedData(historyQuery.data) : null),
    [historyQuery.data],
  )

  const pointCount = historyQuery.data?.items.length ?? 0
  const error = historyQuery.error ?? eventsQuery.error

  return (
    <Flex vertical gap="middle">
      <Typography.Title level={4} style={{ margin: 0 }}>
        {t('history.title')}
      </Typography.Title>

      <Card size="small">
        <Flex wrap gap="middle" align="center" justify="space-between">
          <Segmented
            value={preset === 'custom' ? undefined : preset}
            onChange={(value) => selectPreset(value as RangePreset)}
            options={RANGE_PRESETS.map((p) => ({
              label: t(`history.preset.${p}`),
              value: p,
            }))}
          />
          <RangePicker
            showTime
            value={customRange}
            onChange={selectCustomRange}
            placeholder={[t('history.customFrom'), t('history.customTo')]}
            className="dps-range-picker"
            classNames={{ popup: { root: 'dps-range-popup' } }}
          />
          <Button onClick={handleResetZoom} disabled={!isZoomed}>
            {t('history.resetZoom')}
          </Button>
          <Button onClick={handleExport}>{t('export.button')}</Button>
        </Flex>
      </Card>

      {error !== null && (
        <Alert
          type="error"
          showIcon
          title={apiErrorMessage(t, error)}
          action={
            <Button
              size="small"
              onClick={() => {
                void historyQuery.refetch()
                void eventsQuery.refetch()
              }}
            >
              {t('common.retry')}
            </Button>
          }
        />
      )}

      <Card size="small">
        <Flex vertical gap="small">
          <Flex wrap gap="middle" align="center" justify="space-between">
            <Space size="middle" wrap>
              <Checkbox
                checked={visibleSeries.voltage}
                onChange={(e) =>
                  setVisibleSeries((v) => ({ ...v, voltage: e.target.checked }))
                }
              >
                {t('chart.series.voltage')}
              </Checkbox>
              <Checkbox
                checked={visibleSeries.current}
                onChange={(e) =>
                  setVisibleSeries((v) => ({ ...v, current: e.target.checked }))
                }
              >
                {t('chart.series.current')}
              </Checkbox>
              <Checkbox
                checked={visibleSeries.power}
                onChange={(e) =>
                  setVisibleSeries((v) => ({ ...v, power: e.target.checked }))
                }
              >
                {t('chart.series.power')}
              </Checkbox>
              <Checkbox
                checked={visibleSeries.temperature}
                onChange={(e) =>
                  setVisibleSeries((v) => ({ ...v, temperature: e.target.checked }))
                }
              >
                {t('chart.series.temperature')}
              </Checkbox>
            </Space>
            <Typography.Text type="secondary" className="tabular">
              {historyQuery.data
                ? t('history.pointCount', {
                    count: pointCount,
                    resolution:
                      historyQuery.data.resolution === '1m'
                        ? t('history.resolutionMinute')
                        : t('history.resolutionRaw'),
                  })
                : null}
            </Typography.Text>
          </Flex>

          <Spin spinning={historyQuery.isFetching}>
            {chartData !== null && historyQuery.data !== undefined && pointCount > 0 ? (
              <HistoryChart
                // Remount on theme/locale change so uPlot rebuilds with
                // fresh axis colors and re-localized labels (see the
                // component; colors/labels are captured once at creation).
                key={`${token.colorBgContainer}-${i18n.language}`}
                data={chartData}
                resolution={historyQuery.data.resolution}
                visibleSeries={visibleSeries}
                events={eventsQuery.data?.items ?? []}
                viewRange={viewRange}
                onZoom={handleZoom}
                onResetZoom={handleResetZoom}
              />
            ) : (
              <Empty
                description={
                  historyQuery.isPending ? t('history.loading') : t('history.empty')
                }
              />
            )}
          </Spin>
        </Flex>
      </Card>
    </Flex>
  )
}
