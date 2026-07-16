import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../../test/render'
import { makeActiveStatus } from '../../test/chargeRoutes'
import { ChargeRunView } from './ChargeRunView'

describe('ChargeRunView', () => {
  it('shows glanceable KPIs, the phase and an always-available Stop', () => {
    const onStop = vi.fn()
    renderWithProviders(
      <ChargeRunView
        status={makeActiveStatus()}
        timeoutMs={10_800_000}
        stopping={false}
        onStop={onStop}
      />,
    )

    // KPIs (voltage from the status measurement, delivered mAh).
    expect(screen.getByText('4.05')).toBeInTheDocument()
    expect(screen.getByText('850')).toBeInTheDocument()

    // Phase shown as "X of N" text (shape + text, not colour alone).
    expect(screen.getByText('Пост. ток (CC) (1 из 2)')).toBeInTheDocument()

    // Both safety-cap progress bars are present.
    expect(screen.getByText('Отдано / лимит ёмкости')).toBeInTheDocument()
    expect(screen.getByText('Прошло / таймаут')).toBeInTheDocument()

    const stop = screen.getByRole('button', { name: /Остановить заряд/ })
    fireEvent.click(stop)
    expect(onStop).toHaveBeenCalledTimes(1)
  })

  it('omits the timeout bar when the pre-flight timeout is unknown (e.g. reload)', () => {
    renderWithProviders(
      <ChargeRunView
        status={makeActiveStatus()}
        timeoutMs={null}
        stopping={false}
        onStop={() => undefined}
      />,
    )
    expect(screen.getByText('Отдано / лимит ёмкости')).toBeInTheDocument()
    expect(screen.queryByText('Прошло / таймаут')).toBeNull()
  })
})
