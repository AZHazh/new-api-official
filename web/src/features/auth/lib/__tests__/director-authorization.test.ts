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
import assert from 'node:assert/strict'
import { afterEach, test } from 'node:test'

import {
  clearDirectorAuthorization,
  directorAuthorizationPath,
  readDirectorAuthorization,
  rememberDirectorAuthorization,
} from '../director-authorization'

const origin = 'https://model.example'
const query = new URLSearchParams({
  client_id: 'director-web',
  redirect_uri: 'https://director.example/auth/callback',
  state: 's'.repeat(43),
  code_challenge: 'c'.repeat(43),
  code_challenge_method: 'S256',
})
const path = `/desktop-sync?${query}`
const saved = new Map<string, string>()
const previous = Object.getOwnPropertyDescriptor(globalThis, 'window')

afterEach(() => {
  saved.clear()
  if (previous) Object.defineProperty(globalThis, 'window', previous)
  else Reflect.deleteProperty(globalThis, 'window')
})

test('valid authorization preserves callback, state and S256 as an internal path', () => {
  assert.equal(directorAuthorizationPath(path, origin), path)
  assert.equal(directorAuthorizationPath(origin + path, origin), path)
})

test('external URLs, desktop scope, missing proof and fragments are rejected', () => {
  for (const url of [
    `https://evil.example${path}`,
    `//evil.example${path}`,
    '/dashboard',
    '/desktop-sync?mode=polling',
    path.replace('S256', 'plain'),
    path.replace('state=', 'state=x'),
    `${path}#fragment`,
  ]) {
    assert.equal(directorAuthorizationPath(url, origin), undefined)
  }
})

test('registration and second-factor navigation retain the request until consumed', () => {
  Object.defineProperty(globalThis, 'window', {
    configurable: true,
    value: {
      location: { origin },
      sessionStorage: {
        getItem: (key: string) => saved.get(key) ?? null,
        setItem: (key: string, value: string) => void saved.set(key, value),
        removeItem: (key: string) => void saved.delete(key),
      },
    },
  })
  rememberDirectorAuthorization(path)
  assert.equal(readDirectorAuthorization(), path)
  assert.equal(readDirectorAuthorization(), path)
  clearDirectorAuthorization()
  assert.equal(readDirectorAuthorization(), undefined)
})

test('expired and external saved requests cannot become login destinations', () => {
  Object.defineProperty(globalThis, 'window', {
    configurable: true,
    value: {
      location: { origin },
      sessionStorage: {
        getItem: (key: string) => saved.get(key) ?? null,
        removeItem: (key: string) => void saved.delete(key),
      },
    },
  })
  saved.set(
    'director-authorization-return',
    JSON.stringify({ path, until: Date.now() - 1 })
  )
  assert.equal(readDirectorAuthorization(), undefined)
  saved.set(
    'director-authorization-return',
    JSON.stringify({
      path: `https://evil.example${path}`,
      until: Date.now() + 1000,
    })
  )
  assert.equal(readDirectorAuthorization(), undefined)
})
