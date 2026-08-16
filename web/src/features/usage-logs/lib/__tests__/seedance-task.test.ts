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
import { test } from 'node:test'

import { TASK_ACTIONS, TASK_PLATFORMS, TASK_STATUS } from '../../constants'
import {
  taskActionMapper,
  taskPlatformMapper,
  taskStatusMapper,
} from '../mappers'

test('maps the numeric Seedance task platform to its provider name', () => {
  assert.equal(TASK_PLATFORMS.SEEDANCE, '61')
  assert.equal(taskPlatformMapper.getLabel(TASK_PLATFORMS.SEEDANCE), 'Seedance')
  assert.equal(
    taskActionMapper.getLabel(TASK_ACTIONS.SEEDANCE_CONTEXT_IR),
    'Enhance Video Prompt'
  )
  assert.equal(
    taskStatusMapper.getLabel(TASK_STATUS.SUBMIT_UNKNOWN),
    'Submission uncertain'
  )
})
