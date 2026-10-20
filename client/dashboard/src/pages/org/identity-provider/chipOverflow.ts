/**
 * How many chips fit on one line of `available` pixels, in order. When they
 * don't all fit, room is kept for the overflow chip (`overflowWidth`) that
 * stands in for the rest, so the result can be zero.
 */
export function visibleChipCount(
  widths: number[],
  available: number,
  gap: number,
  overflowWidth: number,
): number {
  const total =
    widths.reduce((sum, width) => sum + width, 0) +
    gap * Math.max(0, widths.length - 1);
  if (total <= available) return widths.length;
  let used = 0;
  for (const [index, width] of widths.entries()) {
    const next = used + (index > 0 ? gap : 0) + width;
    if (next + gap + overflowWidth > available) return index;
    used = next;
  }
  return widths.length;
}
