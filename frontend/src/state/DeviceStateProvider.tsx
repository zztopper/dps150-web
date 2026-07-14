import type { ReactNode } from 'react'
import { useDeviceState } from '../hooks/useDeviceState'
import { DeviceStateContext } from './useDevice'

/**
 * Owns the live device connection (single WebSocket via useDeviceState)
 * and shares the resulting state with the whole route tree, so the
 * connection survives page navigation.
 */
export function DeviceStateProvider({ children }: { children: ReactNode }) {
  const device = useDeviceState()
  return (
    <DeviceStateContext.Provider value={device}>
      {children}
    </DeviceStateContext.Provider>
  )
}
