import { useEffect } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { listEvents, type EventsQuery } from '../api/events'
import { useDevice } from '../state/useDevice'

export const EVENTS_QUERY_KEY = ['events'] as const

/** GET /api/v1/events. 503 storage_unavailable surfaces via `.error`. */
export function useEventsQuery(query: EventsQuery) {
  return useQuery({
    queryKey: [...EVENTS_QUERY_KEY, query],
    queryFn: () => listEvents(query),
    placeholderData: (prev) => prev,
  })
}

/**
 * Invalidates the event journal whenever the live WS stream reports
 * something that the backend also journals: an `event` message
 * (protectionTrip / outputChange / protectionsChanged / profileApplied /
 * meteringSession) or a device link transition (`status`, which the
 * backend journals as deviceConnected/deviceDisconnected). Mount once
 * near the top of EventsPage.
 */
export function useEventsLiveInvalidation(): void {
  const queryClient = useQueryClient()
  const { lastEvent, deviceLink } = useDevice()

  useEffect(() => {
    if (lastEvent === null) {
      return
    }
    void queryClient.invalidateQueries({ queryKey: EVENTS_QUERY_KEY })
    // Only the identity of lastEvent matters — a new WS message means a
    // new reducer object every time. queryClient is a stable singleton.
  }, [lastEvent, queryClient])

  useEffect(() => {
    if (deviceLink === null) {
      return
    }
    void queryClient.invalidateQueries({ queryKey: EVENTS_QUERY_KEY })
  }, [deviceLink, queryClient])
}
