import { useState } from 'react'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { ModalDialog } from './ModalDialog'

function Harness() {
  const [open, setOpen] = useState(false)
  return (
    <div>
      <button type="button" onClick={() => setOpen(true)}>
        Open dialog
      </button>
      {open ? (
        <ModalDialog label="Example dialog" onClose={() => setOpen(false)}>
          <input aria-label="First field" />
          <input aria-label="Second field" />
          <button type="button" onClick={() => setOpen(false)}>
            Done
          </button>
        </ModalDialog>
      ) : null}
    </div>
  )
}

describe('ModalDialog', () => {
  it('moves focus into the dialog on open and back to the opener on close', async () => {
    const user = userEvent.setup()
    render(<Harness />)

    const opener = screen.getByRole('button', { name: 'Open dialog' })
    await user.click(opener)
    expect(screen.getByLabelText('First field')).toHaveFocus()

    await user.keyboard('{Escape}')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(opener).toHaveFocus()
  })

  it('respects a child that claims focus itself via autoFocus', async () => {
    const user = userEvent.setup()
    const onClose = vi.fn()
    render(
      <div>
        <button type="button">Elsewhere</button>
        <ModalDialog label="With autofocus" onClose={onClose}>
          <input aria-label="Plain" />
          <input aria-label="Wanted" autoFocus />
        </ModalDialog>
      </div>,
    )
    expect(screen.getByLabelText('Wanted')).toHaveFocus()
    await user.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalled()
  })

  it('wraps Tab and Shift+Tab at the dialog edges instead of escaping it', async () => {
    const user = userEvent.setup()
    render(<Harness />)
    await user.click(screen.getByRole('button', { name: 'Open dialog' }))

    // Tab from the last element wraps to the first.
    screen.getByRole('button', { name: 'Done' }).focus()
    await user.tab()
    expect(screen.getByLabelText('First field')).toHaveFocus()

    // Shift+Tab from the first element wraps to the last.
    await user.tab({ shift: true })
    expect(screen.getByRole('button', { name: 'Done' })).toHaveFocus()
  })
})
