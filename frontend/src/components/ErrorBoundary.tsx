import { Component, type ErrorInfo, type ReactNode } from 'react'
import { Button, Result } from 'antd'
import { useTranslation } from 'react-i18next'

interface ErrorBoundaryProps {
  children: ReactNode
}

interface ErrorBoundaryState {
  error: Error | null
}

/**
 * Localised fallback for a caught render error. A separate function component so
 * it can use hooks (the boundary itself must be a class); announced via
 * role="alert" with a recovery action.
 */
function ErrorFallback({ error, onReset }: { error: Error; onReset: () => void }) {
  const { t } = useTranslation()
  return (
    <div role="alert">
      <Result
        status="error"
        title={t('errors.boundaryTitle')}
        subTitle={error.message}
        extra={
          <Button type="primary" onClick={onReset}>
            {t('errors.boundaryReset')}
          </Button>
        }
      />
    </div>
  )
}

/**
 * Contains a render-time throw in its subtree so a bug on one page degrades to
 * a recoverable error card instead of unmounting the whole React tree (a white
 * screen). Used to wrap the safety-critical Charge page, whose confirmation and
 * live views must never take the rest of the app down with them.
 */
export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { error: null }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    // Surface for debugging; a real app would forward this to telemetry.
    console.error('ErrorBoundary caught a render error', error, info)
  }

  reset = (): void => {
    this.setState({ error: null })
  }

  render(): ReactNode {
    if (this.state.error !== null) {
      return <ErrorFallback error={this.state.error} onReset={this.reset} />
    }
    return this.props.children
  }
}
