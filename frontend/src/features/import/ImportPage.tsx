import { useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import type { PreviewRow, UploadResult } from '../../api/imports'
import { SelectField } from '../../components/forms/Field'
import { formatDate } from '../../lib/dates'
import { formatMoney } from '../../lib/money'
import { useAccounts } from '../accounts/useAccounts'
import { useCurrentUser } from '../auth/useAuth'
import {
  useCommitImport,
  useDeleteImportBatch,
  useImportBatches,
  useImportPreview,
  useSetImportMapping,
  useUploadImport,
} from './useImports'

type Step = 'upload' | 'mapping' | 'preview' | 'done'

const primaryButtonClass =
  'rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500 focus-visible:ring-2 focus-visible:ring-indigo-500 focus-visible:ring-offset-2 disabled:opacity-50'
const secondaryButtonClass =
  'rounded-md border border-gray-300 px-3 py-1.5 text-sm font-medium text-gray-700 hover:bg-gray-100 focus-visible:ring-2 focus-visible:ring-indigo-500'

// Wizard progress indicator: current step conveyed with text, not just style.
function StepIndicator({ step }: { step: Step }) {
  const steps: { key: Step; label: string }[] = [
    { key: 'upload', label: 'Upload' },
    { key: 'mapping', label: 'Map columns' },
    { key: 'preview', label: 'Preview' },
    { key: 'done', label: 'Done' },
  ]
  const currentIndex = steps.findIndex((s) => s.key === step)
  return (
    <ol className="flex flex-wrap gap-2 text-sm" aria-label="Import steps">
      {steps.map((s, i) => (
        <li
          key={s.key}
          aria-current={s.key === step ? 'step' : undefined}
          className={
            s.key === step
              ? 'font-semibold text-indigo-700'
              : i < currentIndex
                ? 'text-gray-700'
                : 'text-gray-400'
          }
        >
          {i + 1}. {s.label}
        </li>
      ))}
    </ol>
  )
}

interface MappingState {
  accountId: string
  dateColumn: string
  amountColumn: string
  payeeColumn: string
  notesColumn: string
  categoryColumn: string
  dateFormat: string
  amountFormat: string
}

const emptyMapping: MappingState = {
  accountId: '',
  dateColumn: '',
  amountColumn: '',
  payeeColumn: '',
  notesColumn: '',
  categoryColumn: '',
  dateFormat: 'auto',
  amountFormat: 'auto',
}

function toColumn(value: string): number | null {
  return value === '' ? null : Number(value)
}

function rowIssues(row: PreviewRow): { errors: string[]; duplicate: boolean } {
  return {
    errors: row.errors,
    duplicate: (row.duplicateOf?.length ?? 0) > 0 || row.duplicateOfRow !== undefined,
  }
}

export function ImportPage() {
  const { data: user } = useCurrentUser()
  const { data: accounts } = useAccounts()
  const { data: batches } = useImportBatches()
  const upload = useUploadImport()
  const fileInputRef = useRef<HTMLInputElement>(null)

  const [step, setStep] = useState<Step>('upload')
  const [uploadResult, setUploadResult] = useState<UploadResult | null>(null)
  const [mapping, setMapping] = useState<MappingState>(emptyMapping)
  const [mappingErrors, setMappingErrors] = useState<Partial<Record<'accountId' | 'dateColumn' | 'amountColumn', string>>>({})
  const [includeDuplicates, setIncludeDuplicates] = useState(false)
  const [confirmingDelete, setConfirmingDelete] = useState<string | null>(null)

  const batchId = uploadResult?.batch.id ?? null
  const setServerMapping = useSetImportMapping(batchId)
  const preview = useImportPreview(batchId, step === 'preview')
  const commit = useCommitImport(batchId)
  const deleteBatch = useDeleteImportBatch()

  const currency = user?.defaultCurrency ?? 'USD'
  const activeAccounts = (accounts ?? []).filter((a) => !a.archivedAt)

  const onFileChosen = (file: File | undefined) => {
    if (!file) return
    upload.mutate(file, {
      onSuccess: (result) => {
        setUploadResult(result)
        setMapping(emptyMapping)
        setMappingErrors({})
        setStep('mapping')
      },
    })
  }

  const submitMapping = (e: React.FormEvent) => {
    e.preventDefault()
    const errors: typeof mappingErrors = {}
    if (!mapping.accountId) errors.accountId = 'Choose the account these transactions belong to'
    if (mapping.dateColumn === '') errors.dateColumn = 'Choose which column holds the date'
    if (mapping.amountColumn === '') errors.amountColumn = 'Choose which column holds the amount'
    setMappingErrors(errors)
    if (Object.keys(errors).length > 0) return
    setServerMapping.mutate(
      {
        accountId: mapping.accountId,
        dateColumn: toColumn(mapping.dateColumn),
        amountColumn: toColumn(mapping.amountColumn),
        payeeColumn: toColumn(mapping.payeeColumn),
        notesColumn: toColumn(mapping.notesColumn),
        categoryColumn: toColumn(mapping.categoryColumn),
        dateFormat: mapping.dateFormat,
        amountFormat: mapping.amountFormat,
      },
      { onSuccess: () => setStep('preview') },
    )
  }

  const columnOptions = (uploadResult?.columns ?? []).map((name, i) => (
    <option key={i} value={String(i)}>
      {name}
    </option>
  ))

  const columnSelect = (
    label: string,
    key: keyof MappingState,
    error?: string,
    required = false,
  ) => (
    <SelectField
      label={label}
      value={mapping[key]}
      error={error}
      onChange={(e) => setMapping((m) => ({ ...m, [key]: e.target.value }))}
    >
      <option value="">{required ? 'Choose a column…' : 'Not imported'}</option>
      {columnOptions}
    </SelectField>
  )

  return (
    <>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h1 className="text-2xl font-bold text-gray-900">Import transactions</h1>
        <StepIndicator step={step} />
      </div>

      {step === 'upload' ? (
        <section aria-label="Upload" className="mt-6 max-w-xl rounded-lg bg-white p-6 shadow">
          <h2 className="text-lg font-semibold text-gray-900">Upload a CSV file</h2>
          <p className="mt-1 text-sm text-gray-600">
            Export transactions from your bank as CSV, then upload the file here. You will map its
            columns and review every row before anything is saved.
          </p>
          <div className="mt-4">
            <label htmlFor="import-file" className="block text-sm font-medium text-gray-700">
              CSV file
            </label>
            <input
              id="import-file"
              ref={fileInputRef}
              type="file"
              accept=".csv,text/csv"
              onChange={(e) => onFileChosen(e.target.files?.[0])}
              className="mt-1 block w-full text-sm text-gray-700 file:mr-3 file:rounded-md file:border-0 file:bg-indigo-50 file:px-3 file:py-2 file:text-sm file:font-medium file:text-indigo-700 hover:file:bg-indigo-100"
            />
          </div>
          {upload.isPending ? <p className="mt-3 text-sm text-gray-600">Uploading…</p> : null}
          {upload.error ? (
            <p role="alert" className="mt-3 text-sm text-red-600">
              {upload.error.message}
            </p>
          ) : null}
        </section>
      ) : null}

      {step === 'mapping' && uploadResult ? (
        <section aria-label="Map columns" className="mt-6 max-w-2xl rounded-lg bg-white p-6 shadow">
          <h2 className="text-lg font-semibold text-gray-900">
            Map columns from {uploadResult.batch.fileName}
          </h2>
          <p className="mt-1 text-sm text-gray-600">
            {uploadResult.rowCount} rows detected
            {uploadResult.hasHeader ? ' (first row looks like a header)' : ''}.
          </p>

          {uploadResult.sampleRows.length > 0 ? (
            <div className="mt-3 overflow-x-auto rounded-md border border-gray-200">
              <table className="w-full text-sm">
                <caption className="sr-only">Sample rows from the uploaded file</caption>
                <thead className="bg-gray-50">
                  <tr>
                    {uploadResult.columns.map((c, i) => (
                      <th key={i} scope="col" className="px-2 py-1.5 text-left text-xs font-semibold text-gray-600">
                        {c}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-100">
                  {uploadResult.sampleRows.map((row, i) => (
                    <tr key={i}>
                      {row.map((cell, j) => (
                        <td key={j} className="px-2 py-1.5 text-gray-700">
                          {cell}
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}

          <form onSubmit={submitMapping} noValidate className="mt-4 grid gap-3 sm:grid-cols-2">
            <SelectField
              label="Import into account"
              value={mapping.accountId}
              error={mappingErrors.accountId}
              onChange={(e) => setMapping((m) => ({ ...m, accountId: e.target.value }))}
            >
              <option value="">Choose an account…</option>
              {activeAccounts.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.name}
                </option>
              ))}
            </SelectField>
            <div aria-hidden="true" className="hidden sm:block" />
            {columnSelect('Date column', 'dateColumn', mappingErrors.dateColumn, true)}
            {columnSelect('Amount column', 'amountColumn', mappingErrors.amountColumn, true)}
            {columnSelect('Payee column', 'payeeColumn')}
            {columnSelect('Notes column', 'notesColumn')}
            {columnSelect('Category column', 'categoryColumn')}
            <SelectField
              label="Date format"
              value={mapping.dateFormat}
              onChange={(e) => setMapping((m) => ({ ...m, dateFormat: e.target.value }))}
            >
              {uploadResult.dateFormats.map((f) => (
                <option key={f} value={f}>
                  {f === 'auto' ? 'Detect automatically' : f}
                </option>
              ))}
            </SelectField>
            <SelectField
              label="Amount format"
              value={mapping.amountFormat}
              onChange={(e) => setMapping((m) => ({ ...m, amountFormat: e.target.value }))}
            >
              {uploadResult.amountFormats.map((f) => (
                <option key={f} value={f}>
                  {f === 'auto'
                    ? 'Detect automatically'
                    : f === 'dot_decimal'
                      ? 'Dot decimal (1,234.56)'
                      : 'Comma decimal (1.234,56)'}
                </option>
              ))}
            </SelectField>
            {setServerMapping.error ? (
              <p role="alert" className="text-sm text-red-600 sm:col-span-2">
                {setServerMapping.error.message}
              </p>
            ) : null}
            <div className="flex gap-2 sm:col-span-2">
              <button type="submit" disabled={setServerMapping.isPending} className={primaryButtonClass}>
                {setServerMapping.isPending ? 'Saving…' : 'Continue to preview'}
              </button>
              <button type="button" onClick={() => setStep('upload')} className={secondaryButtonClass}>
                Back
              </button>
            </div>
          </form>
        </section>
      ) : null}

      {step === 'preview' ? (
        <section aria-label="Preview" className="mt-6 rounded-lg bg-white p-6 shadow">
          <h2 className="text-lg font-semibold text-gray-900">Preview</h2>
          {preview.isLoading ? <p className="mt-2 text-sm text-gray-600">Checking rows…</p> : null}
          {preview.error ? (
            <p role="alert" className="mt-2 text-sm text-red-600">
              {preview.error.message}
            </p>
          ) : null}
          {preview.data ? (
            <>
              <p className="mt-1 text-sm text-gray-600">
                {preview.data.valid} rows ready · {preview.data.errored} with errors (skipped) ·{' '}
                {preview.data.duplicates} possible duplicates
              </p>
              <div className="mt-3 overflow-x-auto rounded-md border border-gray-200">
                <table className="w-full text-sm">
                  <caption className="sr-only">Rows that will be imported</caption>
                  <thead className="bg-gray-50">
                    <tr>
                      <th scope="col" className="px-2 py-1.5 text-left text-xs font-semibold text-gray-600">Row</th>
                      <th scope="col" className="px-2 py-1.5 text-left text-xs font-semibold text-gray-600">Date</th>
                      <th scope="col" className="px-2 py-1.5 text-right text-xs font-semibold text-gray-600">Amount</th>
                      <th scope="col" className="px-2 py-1.5 text-left text-xs font-semibold text-gray-600">Payee</th>
                      <th scope="col" className="px-2 py-1.5 text-left text-xs font-semibold text-gray-600">Category</th>
                      <th scope="col" className="px-2 py-1.5 text-left text-xs font-semibold text-gray-600">Issues</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-gray-100">
                    {preview.data.rows.map((row) => {
                      const issues = rowIssues(row)
                      return (
                        <tr key={row.index} className={issues.errors.length > 0 ? 'bg-red-50' : ''}>
                          <td className="px-2 py-1.5 text-gray-600">{row.index + 1}</td>
                          <td className="px-2 py-1.5 text-gray-900">
                            {row.date ? formatDate(row.date) : '—'}
                          </td>
                          <td className="px-2 py-1.5 text-right tabular-nums text-gray-900">
                            {row.amount !== undefined ? formatMoney(row.amount, currency) : '—'}
                          </td>
                          <td className="px-2 py-1.5 text-gray-900">{row.payee || '—'}</td>
                          <td className="px-2 py-1.5 text-gray-700">{row.categoryName || '—'}</td>
                          <td className="px-2 py-1.5">
                            {issues.errors.map((e, i) => (
                              <p key={i} className="text-xs text-red-700">
                                <span aria-hidden="true">✕ </span>
                                {e}
                              </p>
                            ))}
                            {issues.duplicate ? (
                              <p className="text-xs font-medium text-amber-800">
                                <span aria-hidden="true">⚠ </span>Possible duplicate
                              </p>
                            ) : null}
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>

              {preview.data.duplicates > 0 ? (
                <label className="mt-3 flex items-center gap-2 text-sm text-gray-700">
                  <input
                    type="checkbox"
                    checked={includeDuplicates}
                    onChange={(e) => setIncludeDuplicates(e.target.checked)}
                    className="h-4 w-4 rounded border-gray-300 text-indigo-600 focus:ring-indigo-500"
                  />
                  Import flagged duplicates anyway
                </label>
              ) : null}

              {commit.error ? (
                <p role="alert" className="mt-3 text-sm text-red-600">
                  {commit.error.message}
                </p>
              ) : null}
              <div className="mt-4 flex gap-2">
                <button
                  type="button"
                  disabled={commit.isPending || preview.data.valid === 0}
                  onClick={() =>
                    commit.mutate(
                      { includeDuplicates, skipRows: [] },
                      { onSuccess: () => setStep('done') },
                    )
                  }
                  className={primaryButtonClass}
                >
                  {commit.isPending ? 'Importing…' : `Import ${preview.data.valid} transactions`}
                </button>
                <button type="button" onClick={() => setStep('mapping')} className={secondaryButtonClass}>
                  Back to mapping
                </button>
              </div>
            </>
          ) : null}
        </section>
      ) : null}

      {step === 'done' && commit.data ? (
        <section aria-label="Import complete" className="mt-6 max-w-xl rounded-lg bg-white p-6 shadow">
          <h2 className="text-lg font-semibold text-gray-900">
            <span aria-hidden="true">✓ </span>Imported {commit.data.createdIds.length} transactions
          </h2>
          <ul className="mt-2 space-y-1 text-sm text-gray-600">
            {commit.data.skippedErrors > 0 ? (
              <li>{commit.data.skippedErrors} rows skipped because of errors.</li>
            ) : null}
            {commit.data.skippedDuplicates > 0 ? (
              <li>{commit.data.skippedDuplicates} duplicates skipped.</li>
            ) : null}
          </ul>
          <div className="mt-4 flex gap-2">
            <Link to="/transactions" className={primaryButtonClass}>
              View transactions
            </Link>
            <button
              type="button"
              onClick={() => {
                setUploadResult(null)
                commit.reset()
                upload.reset()
                if (fileInputRef.current) fileInputRef.current.value = ''
                setStep('upload')
              }}
              className={secondaryButtonClass}
            >
              Import another file
            </button>
          </div>
        </section>
      ) : null}

      {(batches?.length ?? 0) > 0 ? (
        <section aria-label="Previous imports" className="mt-8 max-w-2xl">
          <h2 className="text-lg font-semibold text-gray-900">Previous imports</h2>
          <p className="mt-1 text-sm text-gray-600">
            Deleting an import removes every transaction it created.
          </p>
          <ul className="mt-2 divide-y divide-gray-100 rounded-lg bg-white shadow">
            {batches!.map((b) => (
              <li key={b.id} className="flex flex-wrap items-center justify-between gap-2 px-4 py-3">
                <div>
                  <p className="text-sm font-medium text-gray-900">{b.fileName}</p>
                  <p className="text-xs text-gray-600">
                    {b.status} · {b.rowCount} rows
                  </p>
                </div>
                {confirmingDelete === b.id ? (
                  <span className="flex gap-2">
                    <button
                      type="button"
                      onClick={() =>
                        deleteBatch.mutate(b.id, { onSettled: () => setConfirmingDelete(null) })
                      }
                      disabled={deleteBatch.isPending}
                      className="rounded-md bg-red-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-red-500 disabled:opacity-50"
                    >
                      {deleteBatch.isPending ? 'Deleting…' : 'Confirm delete'}
                    </button>
                    <button
                      type="button"
                      onClick={() => setConfirmingDelete(null)}
                      className={secondaryButtonClass}
                    >
                      Cancel
                    </button>
                  </span>
                ) : (
                  <button
                    type="button"
                    onClick={() => setConfirmingDelete(b.id)}
                    className={secondaryButtonClass}
                  >
                    Delete import
                  </button>
                )}
              </li>
            ))}
          </ul>
        </section>
      ) : null}
    </>
  )
}
