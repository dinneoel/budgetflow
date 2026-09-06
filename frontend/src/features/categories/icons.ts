// Seeded categories use symbolic names; user-created categories store emoji.
const namedIcons: Record<string, string> = {
  home: '🏠',
  zap: '💡',
  droplet: '💧',
  wifi: '📶',
  phone: '📱',
  'shopping-cart': '🛒',
  bus: '🚌',
  utensils: '🍽️',
  heart: '❤️',
  package: '📦',
  film: '🎬',
  repeat: '🔁',
  'shopping-bag': '🛍️',
  plane: '✈️',
  gift: '🎁',
  stethoscope: '🩺',
  shield: '🛡️',
  book: '📚',
  'life-buoy': '🛟',
  'credit-card': '💳',
  'piggy-bank': '🐷',
}

export function categoryIcon(icon: string): string {
  if (Object.hasOwn(namedIcons, icon)) return namedIcons[icon]
  // Keep emoji choices, but never render an unknown name as overflowing text.
  return /^(?:\p{Extended_Pictographic}|\p{Emoji_Presentation})(?:\p{Extended_Pictographic}|\p{Emoji_Presentation}|\p{Emoji_Modifier}|\uFE0F|\u200D)*$/u.test(icon)
    ? icon
    : '🏷️'
}

export const categoryIcons = [...new Set([
  '🏠', '🍎', '🚌', '💡', '🎁', '🎬', '🩺', '📚', '✈️', '💰', '🐾', '🧾',
  ...Object.values(namedIcons),
])]
