import { describe, expect, it } from 'vitest'
import { categoryIcon, categoryIcons } from './icons'

describe('categoryIcon', () => {
  it('resolves seeded names to selectable icons', () => {
    for (const name of ['home', 'zap', 'droplet', 'wifi', 'phone', 'shopping-cart', 'bus', 'utensils', 'heart', 'package', 'film', 'repeat', 'shopping-bag', 'plane', 'gift', 'stethoscope', 'shield', 'book', 'life-buoy', 'credit-card', 'piggy-bank']) {
      expect(categoryIcon(name)).not.toBe('🏷️')
      expect(categoryIcons).toContain(categoryIcon(name))
    }
  })

  it('preserves emoji choices', () => {
    for (const icon of categoryIcons) expect(categoryIcon(icon)).toBe(icon)
  })

  it('uses a compact fallback for empty or unknown names', () => {
    for (const name of ['', 'unknown-icon', 'constructor']) {
      expect(categoryIcon(name)).toBe('🏷️')
    }
  })
})
