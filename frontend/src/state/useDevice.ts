import { createContext, useContext } from 'react'
import type { UseDeviceStateResult } from '../hooks/useDeviceState'

/**
 * App-wide device state shared by the layout (connection badge, global
 * toasts) and the pages. Populated by DeviceStateProvider, which owns
 * the single WebSocket connection (useDeviceState).
 */
export const DeviceStateContext = createContext<UseDeviceStateResult | null>(
  null,
)

/** Device state from the nearest DeviceStateProvider. */
export function useDevice(): UseDeviceStateResult {
  const ctx = useContext(DeviceStateContext)
  if (ctx === null) {
    throw new Error('useDevice must be used within a DeviceStateProvider')
  }
  return ctx
}
