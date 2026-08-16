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
export type VideoSampleLanguage =
  | 'curl'
  | 'python'
  | 'typescript'
  | 'javascript'

export type VideoSampleContext = {
  baseUrl: string
  apiKeyEnv: string
  modelName: string
  endpointPath: string
}

export type VideoApiProtocol =
  | 'video-output'
  | 'video-prompt-enhancer'
  | 'midjourney-video'

const CONTEXT_IR_MODELS = new Set([
  'minmax-h3-context-ir-text',
  'minmax-h3-context-ir-image',
  'minmax-h3-context-ir-multimodal',
])

export function getVideoApiProtocol(modelName: string): VideoApiProtocol {
  if (CONTEXT_IR_MODELS.has(modelName)) return 'video-prompt-enhancer'
  if (modelName === 'midjourney-video') return 'midjourney-video'
  return 'video-output'
}

function buildVideoRequestBody(
  context: VideoSampleContext,
  protocol: VideoApiProtocol
): Record<string, unknown> {
  if (protocol === 'midjourney-video') {
    return {
      prompt: 'The subject slowly turns toward the camera.',
      image_urls: ['https://example.com/reference.png'],
      motion: 'high',
      batch_size: 1,
    }
  }

  const body: Record<string, unknown> = {
    model: context.modelName,
    prompt: 'A cinematic sunrise over a quiet mountain lake.',
  }
  if (protocol !== 'video-prompt-enhancer') return body

  body.seconds = '5'
  if (context.modelName === 'minmax-h3-context-ir-text') {
    body.metadata = { ratio: '16:9' }
  } else {
    body.images = ['https://example.com/reference.png']
  }
  return body
}

export function buildVideoSample(
  language: VideoSampleLanguage,
  context: VideoSampleContext
): string {
  const protocol = getVideoApiProtocol(context.modelName)
  let submitPath = context.endpointPath || '/v1/videos'
  let queryPath = '/v1/videos'
  if (protocol === 'video-prompt-enhancer') {
    submitPath = '/v1/video/generations'
    queryPath = '/v1/video/generations'
  } else if (protocol === 'midjourney-video') {
    submitPath = '/v1/midjourney/generations/video'
    queryPath = '/v1/midjourney/tasks'
  }

  const submitUrl = `${context.baseUrl}${submitPath}`
  const queryUrl = `${context.baseUrl}${queryPath}`
  const body = buildVideoRequestBody(context, protocol)
  const bodyJson = JSON.stringify(body, null, 2)

  if (language === 'curl') {
    return [
      `curl ${submitUrl} \\`,
      `  -H "Authorization: Bearer $${context.apiKeyEnv}" \\`,
      `  -H "Content-Type: application/json" \\`,
      `  -d '${bodyJson.replaceAll('\n', '\n     ')}'`,
      '',
      '# Query with the task ID returned by the submit request.',
      `curl "${queryUrl}/<TASK_ID>" \\`,
      `  -H "Authorization: Bearer $${context.apiKeyEnv}"`,
    ].join('\n')
  }

  if (language === 'python') {
    const output =
      protocol === 'video-prompt-enhancer'
        ? 'print(result.get("data", {}).get("result_text"))'
        : 'print(result)'
    let taskId = 'task_id = task.get("id") or task.get("task_id")'
    if (protocol === 'video-prompt-enhancer') {
      taskId = 'task_id = task.get("data", {}).get("task_id")'
    } else if (protocol === 'midjourney-video') {
      taskId = [
        'task_entries = task.get("data", [])',
        'task_id = task_entries[0].get("task_id") if task_entries else None',
      ].join('\n')
    }
    return [
      'import requests',
      '',
      'headers = {"Authorization": "Bearer <YOUR_API_KEY>"}',
      `created = requests.post("${submitUrl}", json=${JSON.stringify(body)}, headers=headers)`,
      'created.raise_for_status()',
      'task = created.json()',
      taskId,
      '',
      `response = requests.get(f"${queryUrl}/{task_id}", headers=headers)`,
      'response.raise_for_status()',
      'result = response.json()',
      output,
    ].join('\n')
  }

  const resultOutput =
    protocol === 'video-prompt-enhancer'
      ? 'console.log(result.data?.result_text)'
      : 'console.log(result)'
  let typedTask = ''
  if (language === 'typescript') {
    if (protocol === 'video-prompt-enhancer') {
      typedTask = ' as { data?: { task_id?: string } }'
    } else if (protocol === 'midjourney-video') {
      typedTask = ' as { data?: Array<{ task_id?: string }> }'
    } else {
      typedTask = ' as { id?: string; task_id?: string }'
    }
  }
  let taskId = 'const taskId = task.id ?? task.task_id'
  if (protocol === 'video-prompt-enhancer') {
    taskId = 'const taskId = task.data?.task_id'
  } else if (protocol === 'midjourney-video') {
    taskId = 'const taskId = task.data?.[0]?.task_id'
  }
  return [
    'const headers = {',
    `  Authorization: \`Bearer \${process.env.${context.apiKeyEnv}}\`,`,
    `  'Content-Type': 'application/json',`,
    '}',
    '',
    `const created = await fetch('${submitUrl}', {`,
    `  method: 'POST',`,
    '  headers,',
    `  body: JSON.stringify(${JSON.stringify(body)}),`,
    '})',
    `const task = (await created.json())${typedTask}`,
    taskId,
    '',
    `const response = await fetch(\`${queryUrl}/\${taskId}\`, { headers })`,
    'const result = await response.json()',
    resultOutput,
  ].join('\n')
}
