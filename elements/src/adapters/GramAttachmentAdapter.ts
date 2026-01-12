import { Auth } from '@/hooks/useAuth'
import {
  Attachment,
  AttachmentAdapter,
  CompleteAttachment,
  PendingAttachment,
  ThreadUserMessagePart,
} from '@assistant-ui/react'

export class GramAttachmentAdapter implements AttachmentAdapter {
  #apiUrl: string
  #authHeaders: Auth['headers']
  #uploads = new Map<string, string>()
  public accept = [
    'text/plain',
    'text/html',
    'text/markdown',
    'text/csv',
    'text/xml',
    'text/json',
    'text/css',
    'image/*',
    'application/pdf',
    'application/json',
    'application/yaml',
    'application/x-yaml',
  ].join(',')

  constructor(apiUrl: string, authHeaders: Auth['headers']) {
    this.#apiUrl = apiUrl
    this.#authHeaders = authHeaders
  }

  public async *add({
    file,
  }: {
    file: File
  }): AsyncGenerator<PendingAttachment, void> {
    const id = crypto.randomUUID()
    const attachment: PendingAttachment = {
      id,
      type: typeFromMimeType(file.type),
      name: file.name,
      contentType: file.type,
      file,
      status: { type: 'running', reason: 'uploading', progress: 0 },
    }

    if (!this.#authHeaders) {
      console.error('No auth headers available for attachment upload')
      yield {
        ...attachment,
        status: { type: 'incomplete', reason: 'error' },
      }
      return
    } else {
      yield attachment
    }

    let response: Response
    try {
      response = await fetch(
        this.#apiUrl + '/rpc/assets.uploadChatAttachment',
        {
          method: 'POST',
          body: file,
          headers: {
            ...this.#authHeaders,
            'content-type': file.type,
            'content-length': file.size.toString(),
          },
        }
      )
    } catch (err: unknown) {
      console.error('Failed to upload attachment', err)
      yield {
        ...attachment,
        status: { type: 'incomplete', reason: 'error' },
      }
      return
    }

    if (!response.ok) {
      let msg = await response.text().catch(() => '')
      msg ||= response.statusText
      console.error(`Failed to upload attachment: ${msg}`)
      yield {
        ...attachment,
        status: { type: 'incomplete', reason: 'error' },
      }
      return
    }

    try {
      const data = await response.json()
      this.#uploads.set(id, this.#apiUrl + data.url)
      yield {
        ...attachment,
        status: { type: 'requires-action', reason: 'composer-send' },
      }
    } catch (err: unknown) {
      console.error('Failed to parse upload response', err)
      yield {
        ...attachment,
        status: { type: 'incomplete', reason: 'error' },
      }
      return
    }
  }

  public async remove(attachment: Attachment): Promise<void> {
    this.#uploads.delete(attachment.id)
  }

  public async send(
    attachment: PendingAttachment
  ): Promise<CompleteAttachment> {
    const url = this.#uploads.get(attachment.id)
    if (!url) throw new Error('Attachment not uploaded')

    const res = await fetch(url, { headers: this.#authHeaders })
    if (!res.ok) {
      let msg = await res.text().catch(() => '')
      msg ||= res.statusText
      throw new Error(`Failed to access uploaded attachment: ${msg}`)
    }
    const blob = await res.blob()
    const objURL = URL.createObjectURL(blob)

    this.#uploads.delete(attachment.id)

    let content: ThreadUserMessagePart[]
    if (attachment.type === 'image') {
      content = [{ type: 'image', image: objURL, filename: attachment.name }]
    } else {
      content = [
        {
          type: 'file',
          data: objURL,
          mimeType: attachment.contentType,
          filename: attachment.name,
        },
      ]
    }

    return {
      ...attachment,
      status: { type: 'complete' },
      content,
    }
  }
}

function typeFromMimeType(mimeType: string): 'image' | 'document' | 'file' {
  switch (true) {
    case mimeType.startsWith('image/'):
      return 'image'
    case mimeType.startsWith('text/') ||
      mimeType === 'application/pdf' ||
      mimeType === 'application/json' ||
      mimeType === 'application/yaml' ||
      mimeType === 'application/x-yaml':
      return 'document'
    default:
      return 'file'
  }
}
