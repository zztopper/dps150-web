import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, test } from 'vitest'
import { renderWithProviders } from './test/render'
import App from './App'

describe('App shell', () => {
  test('renders the layout: title, connection badge and navigation', () => {
    renderWithProviders(<App />)
    expect(screen.getByText('Управление DPS-150')).toBeInTheDocument()
    expect(screen.getByText('Нет связи с сервером')).toBeInTheDocument()
    // The dashboard is the index route.
    expect(screen.getByText('Уставки и выход')).toBeInTheDocument()
  })

  test('menu click navigates to a stub page', () => {
    renderWithProviders(<App />)

    fireEvent.click(screen.getByRole('link', { name: 'История' }))
    expect(screen.getByText('История измерений')).toBeInTheDocument()
    // HistoryPage (F-013) is implemented: the day/week/month presets
    // are the reliable "this is the real page" signal (jsdom has no
    // canvas, so the chart itself does not render here).
    expect(screen.getByText('Сутки')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('link', { name: 'Настройки' }))
    expect(
      screen.getByText(
        'Раздел в разработке: здесь появятся настройки уведомлений',
      ),
    ).toBeInTheDocument()

    // Back to the dashboard.
    fireEvent.click(screen.getByRole('link', { name: 'Дашборд' }))
    expect(screen.getByText('Уставки и выход')).toBeInTheDocument()
  })

  test('deep link renders the stub page directly', () => {
    renderWithProviders(<App />, { route: '/events' })
    expect(screen.getByText('Журнал событий')).toBeInTheDocument()
    expect(
      screen.getByText('Раздел в разработке: здесь появится журнал событий'),
    ).toBeInTheDocument()
  })
})
