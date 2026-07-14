/* oxlint-disable react/only-export-components -- test helper, not fast-refreshed */
import type { ReactElement, ReactNode } from 'react'
import { render } from '@testing-library/react'
import { App as AntApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { DeviceStateProvider } from '../state/DeviceStateProvider'
import '../i18n'

export interface RenderWithProvidersOptions {
  /** Initial route for the MemoryRouter (default "/"). */
  route?: string
}

export function renderWithProviders(
  ui: ReactElement,
  { route = '/' }: RenderWithProvidersOptions = {},
) {
  function Providers({ children }: { children: ReactNode }) {
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    return (
      <MemoryRouter initialEntries={[route]}>
        <QueryClientProvider client={queryClient}>
          <AntApp>
            <DeviceStateProvider>{children}</DeviceStateProvider>
          </AntApp>
        </QueryClientProvider>
      </MemoryRouter>
    )
  }
  return render(ui, { wrapper: Providers })
}
