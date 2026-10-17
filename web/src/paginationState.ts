export function pageCount(total: number): number {
  return Math.min(50001, Math.max(1, Math.ceil(total / 20)))
}

export function pageNumbers(current: number, count: number): (number | 'gap-before' | 'gap-after')[] {
  if (count <= 7) return Array.from({ length: count }, (_, index) => index + 1)
  const start = Math.max(2, Math.min(current - 1, count - 4))
  const end = Math.min(count - 1, Math.max(current + 1, 5))
  return [1, ...(start > 2 ? ['gap-before' as const] : []),
    ...Array.from({ length: end - start + 1 }, (_, index) => start + index),
    ...(end < count - 1 ? ['gap-after' as const] : []), count]
}

export function pageOffset(value: string, count: number): number | null {
  if (!/^\d+$/.test(value)) return null
  const page = Number(value)
  if (!Number.isSafeInteger(page) || page < 1 || page > count || page > 50001) return null
  return (page - 1) * 20
}
