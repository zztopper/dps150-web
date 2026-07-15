// API access tokens (F-020). Mirrors docs/architecture/api-contract.md,
// "API tokens (F-020)" section of API contract v3. Management is exposed
// ONLY behind the browser UI (Authelia/forward-auth per ADR-006) — these
// endpoints are never reachable with a Bearer token.
import { apiRequest } from './client'

export type TokenScope = 'read' | 'control'

/** Token metadata only — the backend never returns the secret again. */
export interface ApiToken {
  id: number
  name: string
  scope: TokenScope
  createdAt: number
  lastUsedAt: number | null
}

export interface TokensPage {
  items: ApiToken[]
}

/** Body of POST /api/v1/tokens. */
export interface CreateTokenInput {
  name: string
  scope: TokenScope
}

/**
 * 201 response of POST /api/v1/tokens: the new token's metadata plus its
 * bearer secret. The secret is shown this ONE time — it is generated from a
 * one-way hash and cannot be recovered afterwards.
 */
export interface CreateTokenResponse extends ApiToken {
  token: string
}

/** GET /api/v1/tokens — every token's metadata, oldest first. */
export function listTokens(): Promise<TokensPage> {
  return apiRequest<TokensPage>('/api/v1/tokens')
}

/** POST /api/v1/tokens — 400 invalid_token on a bad name/scope. */
export function createToken(input: CreateTokenInput): Promise<CreateTokenResponse> {
  return apiRequest<CreateTokenResponse>('/api/v1/tokens', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

/** DELETE /api/v1/tokens/{id} — revokes the token immediately. */
export function deleteToken(id: number): Promise<void> {
  return apiRequest<void>(`/api/v1/tokens/${id}`, { method: 'DELETE' })
}
