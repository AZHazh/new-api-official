/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
const storageKey = 'director-authorization-return'
const maxAge = 10 * 60 * 1000

// Preserve only a same-origin, well-formed Cloud Director authorization request.
// The model station backend still checks the registered client and callback.
export function directorAuthorizationPath(
  value: string,
  origin: string
): string | undefined {
  try {
    const url = new URL(value, origin)
    if (
      url.origin !== origin ||
      url.pathname !== '/desktop-sync' ||
      url.username ||
      url.password ||
      url.hash ||
      !/^[a-zA-Z0-9_-]{1,80}$/.test(url.searchParams.get('client_id') || '') ||
      !url.searchParams.get('redirect_uri') ||
      !/^[a-zA-Z0-9_-]{43}$/.test(url.searchParams.get('state') || '') ||
      !/^[a-zA-Z0-9_-]{43}$/.test(
        url.searchParams.get('code_challenge') || ''
      ) ||
      url.searchParams.get('code_challenge_method') !== 'S256'
    ) {
      return undefined
    }
    return url.pathname + url.search
  } catch {
    return undefined
  }
}

export function rememberDirectorAuthorization(value: string): void {
  try {
    const path = directorAuthorizationPath(value, window.location.origin)
    if (path) {
      window.sessionStorage.setItem(
        storageKey,
        JSON.stringify({ path, until: Date.now() + maxAge })
      )
    }
  } catch {
    /* Storage may be disabled; normal password redirects still work. */
  }
}

export function readDirectorAuthorization(): string | undefined {
  try {
    const saved: { path?: unknown; until?: unknown } = JSON.parse(
      window.sessionStorage.getItem(storageKey) || '{}'
    )
    if (
      typeof saved.path === 'string' &&
      typeof saved.until === 'number' &&
      saved.until > Date.now() &&
      saved.until <= Date.now() + maxAge
    ) {
      return directorAuthorizationPath(saved.path, window.location.origin)
    }
    clearDirectorAuthorization()
  } catch {
    /* Ignore invalid or unavailable storage. */
  }
  return undefined
}

export function clearDirectorAuthorization(): void {
  try {
    window.sessionStorage.removeItem(storageKey)
  } catch {
    /* Storage is optional. */
  }
}
