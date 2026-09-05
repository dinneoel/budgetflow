import { useEffect, useRef, type KeyboardEvent, type ReactNode } from 'react'

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

// Modal overlay with the keyboard contract dialogs need: focus moves into the
// dialog on open (unless a child used autoFocus first), Tab cycles inside it,
// Escape closes it, and focus returns to the opening control on close.
export function ModalDialog({
  label,
  onClose,
  className,
  children,
}: {
  label: string
  onClose: () => void
  className?: string
  children: ReactNode
}) {
  const dialogRef = useRef<HTMLDivElement>(null)
  // Captured during the first render, before React commits any autoFocus.
  const openerRef = useRef<HTMLElement | null>(
    document.activeElement instanceof HTMLElement ? document.activeElement : null,
  )

  useEffect(() => {
    const dialog = dialogRef.current
    if (dialog && !dialog.contains(document.activeElement)) {
      dialog.querySelector<HTMLElement>(FOCUSABLE)?.focus()
    }
    const opener = openerRef.current
    return () => opener?.focus()
  }, [])

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === 'Escape') {
      e.stopPropagation()
      onClose()
      return
    }
    if (e.key !== 'Tab') {
      return
    }
    const dialog = dialogRef.current
    if (!dialog) {
      return
    }
    const focusable = Array.from(dialog.querySelectorAll<HTMLElement>(FOCUSABLE))
    if (focusable.length === 0) {
      return
    }
    const first = focusable[0]
    const last = focusable[focusable.length - 1]
    const active = document.activeElement
    if (e.shiftKey && (active === first || !dialog.contains(active))) {
      e.preventDefault()
      last.focus()
    } else if (!e.shiftKey && active === last) {
      e.preventDefault()
      first.focus()
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center overflow-y-auto bg-black/30 p-4">
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-label={label}
        onKeyDown={onKeyDown}
        className={['w-full rounded-lg bg-white p-4 shadow-lg', className ?? ''].join(' ')}
      >
        {children}
      </div>
    </div>
  )
}
