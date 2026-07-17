import { Descriptions, Empty, Flex, Tooltip, Typography } from 'antd'
import { InfoCircleOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import type { IVComponent, IVMetrics as IVMetricsData } from '../../api/iv'
import { metricQuality, metricRows, metricUnitSuffix, metricView } from './ivFormat'

export interface IVMetricsViewProps {
  metrics: IVMetricsData | null
  component: IVComponent
}

/**
 * The analysis metrics of a finalized sweep, rendered null-safe (F-024, design
 * §3.8). A metric that is `null` or `quality:"unreliable"` renders as "—" with
 * an accessible "не определено" label and the reason in a tooltip — NEVER `0`.
 * An `approx` metric (e.g. `ideality`, approximate by construction) is prefixed
 * with an "≈" marker and an explanatory tooltip. The `notes` reasons are listed
 * below so the operator sees exactly why a metric was withheld.
 */
export function IVMetricsView({ metrics, component }: IVMetricsViewProps) {
  const { t } = useTranslation()

  if (metrics === null) {
    return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('iv.metrics.pending')} />
  }

  const rows = metricRows(metrics, component)
  const notes = metrics.notes ?? []
  const notesText = notes.length > 0 ? notes.join(' · ') : t('iv.metrics.notDeterminedHint')

  if (rows.length === 0) {
    return (
      <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('iv.metrics.noneForComponent')} />
    )
  }

  return (
    <Flex vertical gap="middle">
      <Descriptions
        size="small"
        column={{ xs: 1, sm: 2 }}
        bordered
        items={rows.map((row) => {
          const view = metricView(row.value, metricQuality(metrics, row.key))
          const label = t('iv.metrics.' + row.key)
          if (!view.available) {
            return {
              key: row.key,
              label,
              children: (
                <Tooltip title={notesText}>
                  <Typography.Text type="secondary" aria-label={t('iv.metrics.notDetermined')}>
                    —
                  </Typography.Text>
                </Tooltip>
              ),
            }
          }
          // `view.available` implies a finite number here.
          const text = `${row.format(row.value as number)}${metricUnitSuffix(t, row.unit)}`
          return {
            key: row.key,
            label,
            children: view.approx ? (
              <Tooltip title={t('iv.metrics.approxHint')}>
                <span className="tabular">
                  ≈ {text}{' '}
                  <InfoCircleOutlined style={{ opacity: 0.55 }} aria-label={t('iv.metrics.approx')} />
                </span>
              </Tooltip>
            ) : (
              <span className="tabular">{text}</span>
            ),
          }
        })}
      />

      {notes.length > 0 && (
        <div>
          <Typography.Text strong>{t('iv.metrics.notesTitle')}</Typography.Text>
          <ul style={{ margin: '4px 0 0', paddingInlineStart: 20 }}>
            {notes.map((n, i) => (
              <li key={i}>
                <Typography.Text type="secondary">{n}</Typography.Text>
              </li>
            ))}
          </ul>
        </div>
      )}
    </Flex>
  )
}
