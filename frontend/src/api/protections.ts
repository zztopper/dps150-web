// Protection thresholds (F-014). Mirrors
// docs/architecture/api-contract.md, "Protection setpoints (F-014)".
import type { Protections } from './types'
import { apiRequest } from './client'

/** Body of PUT /api/v1/device/protections — any non-empty subset. */
export type ProtectionsInput = Partial<Protections>

/** PUT /api/v1/device/protections — returns all five effective values. */
export function putProtections(input: ProtectionsInput): Promise<Protections> {
  return apiRequest<Protections>('/api/v1/device/protections', {
    method: 'PUT',
    body: JSON.stringify(input),
  })
}
