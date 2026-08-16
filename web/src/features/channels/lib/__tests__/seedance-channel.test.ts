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
import { describe, test } from 'node:test'

import {
  CHANNEL_TYPE_OPTIONS,
  CHANNEL_TYPE_SEEDANCE,
  MODEL_FETCHABLE_TYPES,
} from '../../constants'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  transformFormDataToCreatePayload,
} from '../channel-form'
import { getChannelTypeConfig } from '../channel-type-config'
import { getChannelTypeIcon, getKeyPromptForType } from '../channel-utils'

function seedanceForm() {
  return {
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'Seedance upstream',
    type: CHANNEL_TYPE_SEEDANCE,
    base_url: 'https://api.seedance.nz',
    key: 'seedance-test-key',
    models: 'seedance-2.0-mini-t2v',
  }
}

describe('Seedance channel', () => {
  test('registers channel selection, model discovery, and provider metadata', () => {
    assert.deepEqual(
      CHANNEL_TYPE_OPTIONS.find(
        (option) => option.value === CHANNEL_TYPE_SEEDANCE
      ),
      { value: CHANNEL_TYPE_SEEDANCE, label: 'Seedance' }
    )
    assert.equal(MODEL_FETCHABLE_TYPES.has(CHANNEL_TYPE_SEEDANCE), true)
    assert.equal(getChannelTypeIcon(CHANNEL_TYPE_SEEDANCE), 'CogVideo')
    assert.equal(
      getKeyPromptForType(CHANNEL_TYPE_SEEDANCE),
      'Enter Seedance API key'
    )
    assert.equal(
      getChannelTypeConfig(CHANNEL_TYPE_SEEDANCE).defaultBaseUrl,
      'https://api.seedance.nz'
    )
  })

  test('rejects batch and newline-delimited API keys before submission', () => {
    const batchResult = channelFormSchema.safeParse({
      ...seedanceForm(),
      multi_key_mode: 'multi_to_single',
    })
    const newlineResult = channelFormSchema.safeParse({
      ...seedanceForm(),
      key: 'first-key\nsecond-key',
    })

    assert.equal(batchResult.success, false)
    assert.equal(newlineResult.success, false)
  })

  test('creates a single-key payload even when called with stale form state', () => {
    const form = seedanceForm()
    assert.equal(channelFormSchema.safeParse(form).success, true)

    const payload = transformFormDataToCreatePayload({
      ...form,
      multi_key_mode: 'batch',
    })

    assert.equal(payload.mode, 'single')
    assert.equal(payload.multi_key_mode, undefined)
    assert.equal(payload.channel.key, 'seedance-test-key')
  })
})
