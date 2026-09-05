import { post, setCsrfToken } from './client'

// Permanently deletes the signed-in user's account after re-authentication.
export async function deleteAccount(password: string): Promise<void> {
  await post<void>('/account/delete', { password })
  setCsrfToken(null)
}

// Direct-download URL for the full data export ZIP (browser navigation,
// not a JSON fetch — the session cookie authenticates the request).
export const fullExportUrl = '/api/export/all.zip'
