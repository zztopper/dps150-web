/* oxlint-disable react/only-export-components -- test helper, not fast-refreshed */
import type { ReactElement, ReactNode } from 'react'
import { render } from '@testing-library/react'
import { App as AntApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import '../i18n'

function Providers({ children }: { children: ReactNode }) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
  return (
    <QueryClientProvider client={queryClient}>
      <AntApp>{children}</AntApp>
    </QueryClientProvider>
  )
}

export function renderWithProviders(ui: ReactElement) {
  return render(ui, { wrapper: Providers })
}
