import { describe, expect, it } from 'vitest'
import { formatMoney, minorToInputString, parseMoneyInput } from './money'

describe('parseMoneyInput', () => {
  it('parses decimals into minor units', () => {
    expect(parseMoneyInput('1250.00')).toBe(125000)
    expect(parseMoneyInput('0.5')).toBe(50)
    expect(parseMoneyInput('12')).toBe(1200)
    expect(parseMoneyInput('1,234.56')).toBe(123456)
    expect(parseMoneyInput('-42.07')).toBe(-4207)
  })

  it('rejects malformed input', () => {
    expect(parseMoneyInput('')).toBeNull()
    expect(parseMoneyInput('abc')).toBeNull()
    expect(parseMoneyInput('1.234')).toBeNull()
    expect(parseMoneyInput('1.2.3')).toBeNull()
  })
})

describe('minorToInputString', () => {
  it('renders minor units as a plain decimal', () => {
    expect(minorToInputString(125000)).toBe('1250.00')
    expect(minorToInputString(5)).toBe('0.05')
    expect(minorToInputString(-4207)).toBe('-42.07')
    expect(minorToInputString(0)).toBe('0.00')
  })
})

describe('formatMoney', () => {
  it('formats with the currency symbol', () => {
    expect(formatMoney(125000, 'USD')).toBe('$1,250.00')
  })

  it('falls back to a plain decimal for unknown codes', () => {
    expect(formatMoney(500, 'NOPE')).toBe('5.00 NOPE')
  })
})
