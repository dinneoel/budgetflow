import { ApiError, CSRF_HEADER, del, get, getCsrfToken, post, put } from './client'

export interface ImportBatch {
  id: string
  fileName: string
  status: string
  rowCount: number
  accountId: string | null
  createdAt: string
  committedAt: string | null
}

export interface UploadResult {
  batch: ImportBatch
  columns: string[]
  hasHeader: boolean
  rowCount: number
  sampleRows: string[][]
  dateFormats: string[]
  amountFormats: string[]
}

export interface ImportMapping {
  accountId: string
  dateColumn: number | null
  amountColumn: number | null
  payeeColumn: number | null
  notesColumn: number | null
  categoryColumn: number | null
  dateFormat: string
  amountFormat: string
}

export interface PreviewRow {
  index: number
  raw: string[]
  date?: string
  amount?: number
  type?: string
  payee: string
  notes: string
  categoryName?: string
  categoryId?: string
  errors: string[]
  warnings: string[]
  duplicateOf: string[] | null
  duplicateOfRow?: number
}

export interface ImportPreview {
  rows: PreviewRow[]
  valid: number
  errored: number
  duplicates: number
}

export interface CommitInput {
  includeDuplicates: boolean
  skipRows: number[]
}

export interface CommitResult {
  batch: ImportBatch
  createdIds: string[]
  skippedErrors: number
  skippedDuplicates: number
  skippedManually: number
}

// Uploads the CSV as multipart form data. This bypasses the JSON client, so
// it applies the same CSRF header and error normalization itself.
export async function uploadImport(file: File): Promise<UploadResult> {
  const form = new FormData()
  form.append('file', file)
  const headers: Record<string, string> = {}
  const token = getCsrfToken()
  if (token) {
    headers[CSRF_HEADER] = token
  }
  const res = await fetch('/api/imports', {
    method: 'POST',
    headers,
    body: form,
    credentials: 'same-origin',
  })
  const data: unknown = await res.json().catch(() => null)
  if (!res.ok) {
    const message =
      data !== null &&
      typeof data === 'object' &&
      'error' in data &&
      typeof (data as { error: unknown }).error === 'string'
        ? (data as { error: string }).error
        : `Upload failed (${res.status})`
    throw new ApiError(res.status, message)
  }
  return data as UploadResult
}

export async function listImportBatches(signal?: AbortSignal): Promise<ImportBatch[]> {
  const res = await get<{ batches: ImportBatch[] }>('/imports', signal)
  return res.batches
}

export function setImportMapping(
  batchId: string,
  mapping: ImportMapping,
): Promise<{ batch: ImportBatch; mapping: ImportMapping }> {
  return put(`/imports/${batchId}/mapping`, mapping)
}

export function getImportPreview(batchId: string, signal?: AbortSignal): Promise<ImportPreview> {
  return get<ImportPreview>(`/imports/${batchId}/preview`, signal)
}

export function commitImport(batchId: string, input: CommitInput): Promise<CommitResult> {
  return post(`/imports/${batchId}/commit`, input)
}

export function deleteImportBatch(
  batchId: string,
): Promise<{ batch: ImportBatch; deletedTransactionIds: string[] }> {
  return del(`/imports/${batchId}`)
}
