// Saved profiles (F-010) and hardware presets M1-M6 (F-011).
// Mirrors docs/architecture/api-contract.md, "API contract v2" section.
import type { Protections } from './types'
import { apiRequest } from './client'

export interface Profile {
  id: number
  name: string
  voltage: number
  current: number
  protections: Protections
  createdAt: number
  updatedAt: number
}

/** Body of POST /api/v1/profiles and PUT /api/v1/profiles/{id}. */
export interface ProfileInput {
  name: string
  voltage: number
  current: number
  protections: Protections
}

export interface ProfilesPage {
  items: Profile[]
}

export interface PresetSlot {
  slot: number
  voltage: number
  current: number
}

export interface PresetsPage {
  items: PresetSlot[]
}

/** Body of PUT /api/v1/device/presets/{slot} — the two forms are mutually exclusive. */
export type PresetAssignment =
  | { profileId: number }
  | { voltage: number; current: number }

/** GET /api/v1/profiles — every profile, sorted by name. */
export function listProfiles(): Promise<ProfilesPage> {
  return apiRequest<ProfilesPage>('/api/v1/profiles')
}

/** POST /api/v1/profiles — 409 profile_name_taken on duplicate name. */
export function createProfile(input: ProfileInput): Promise<Profile> {
  return apiRequest<Profile>('/api/v1/profiles', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

/** PUT /api/v1/profiles/{id} — 404 profile_not_found. */
export function updateProfile(id: number, input: ProfileInput): Promise<Profile> {
  return apiRequest<Profile>(`/api/v1/profiles/${id}`, {
    method: 'PUT',
    body: JSON.stringify(input),
  })
}

/** DELETE /api/v1/profiles/{id}. */
export function deleteProfile(id: number): Promise<void> {
  return apiRequest<void>(`/api/v1/profiles/${id}`, { method: 'DELETE' })
}

/**
 * POST /api/v1/profiles/{id}/apply — writes voltage/current + all five
 * protections to the device. INVARIANT: never touches the output relay.
 */
export function applyProfile(id: number): Promise<{ applied: boolean }> {
  return apiRequest<{ applied: boolean }>(`/api/v1/profiles/${id}/apply`, {
    method: 'POST',
  })
}

/** GET /api/v1/device/presets — the six M1..M6 hardware slots. */
export function listPresets(): Promise<PresetsPage> {
  return apiRequest<PresetsPage>('/api/v1/device/presets')
}

/**
 * PUT /api/v1/device/presets/{slot} — only voltage/current reach the
 * hardware slot (the device does not store protections in presets).
 */
export function putPreset(slot: number, assignment: PresetAssignment): Promise<PresetSlot> {
  return apiRequest<PresetSlot>(`/api/v1/device/presets/${slot}`, {
    method: 'PUT',
    body: JSON.stringify(assignment),
  })
}
