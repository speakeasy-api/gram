// Deterministic gradient colors from any string label (project/org/assistant
// id, member id). Colors are drawn from the Speakeasy brand spectrum and kept
// subtle: a single brand hue with a small drift + lightness delta, rather than
// two clashing random hues.

// Brand spectrum hues (h, s) — the same muted brand palette used elsewhere in
// the app (e.g. the source activity bars), derived from the brand gradient.
const BRAND_HUES: { h: number; s: number }[] = [
  { h: 214, s: 48 }, // blue
  { h: 4, s: 45 }, // red
  { h: 108, s: 28 }, // green
  { h: 23, s: 52 }, // orange
  { h: 334, s: 36 }, // magenta
  { h: 68, s: 34 }, // lime
  { h: 154, s: 36 }, // teal
  { h: 220, s: 44 }, // indigo
  { h: 280, s: 32 }, // purple
];

// FNV-1a hash for good distribution across short labels.
function fnv1a(str: string): number {
  let hash = 2166136261;
  for (let i = 0; i < str.length; i++) {
    hash ^= str.charCodeAt(i);
    hash +=
      (hash << 1) + (hash << 4) + (hash << 7) + (hash << 8) + (hash << 24);
  }
  return hash >>> 0;
}

export function getGradientColors(label: string): {
  from: string;
  to: string;
  angle: number;
} {
  const hash = fnv1a(label);
  const base = BRAND_HUES[hash % BRAND_HUES.length]!;

  // Small hue drift keeps the gradient lively without clashing.
  // Use unsigned shifts: the FNV-1a hash sets bit 31, and a signed `>>`
  // would yield negative intermediates that skew drift/angle.
  const drift = 12 + ((hash >>> 10) % 12); // 12–23°
  // A few pleasing diagonals.
  const angle = 130 + ((hash >>> 18) % 3) * 15; // 130 / 145 / 160

  return {
    from: `hsl(${base.h}, ${base.s}%, 60%)`,
    to: `hsl(${(base.h + drift) % 360}, ${Math.max(34, base.s - 8)}%, 48%)`,
    angle,
  };
}

/**
 * Deterministic muted identity tint for avatars/initials: a soft brand-hue
 * wash with a deep same-hue foreground. Solid (no gradient) and desaturated
 * to sit inside the editorial palette.
 */
export function getIdentityTint(label: string): {
  backgroundColor: string;
  color: string;
} {
  const base = BRAND_HUES[fnv1a(label) % BRAND_HUES.length]!;
  return {
    backgroundColor: `hsl(${base.h}, ${Math.min(base.s, 30)}%, 90%)`,
    color: `hsl(${base.h}, ${Math.min(base.s + 10, 45)}%, 28%)`,
  };
}

/**
 * Deterministic accent colors for a set of filter dimensions, one per key.
 *
 * The hue is derived from the dimension's own id, so a given filter keeps the
 * same color wherever it appears — "Status" is the same swatch on every page
 * that filters by it, which is what makes the color worth reading at all.
 *
 * Hashing alone would let two dimensions in the same bar collide, so a taken
 * hue probes forward to the next free one. Assignment walks the keys in render
 * order, making the result stable for a given set rather than dependent on
 * which chip happened to resolve first. Past nine dimensions the palette is
 * exhausted and hues repeat, which is the honest outcome — a bar that wide has
 * bigger problems than a duplicate swatch.
 */
export function getFilterAccents(keys: string[]): Record<string, string> {
  const taken = new Set<number>();
  const accents: Record<string, string> = {};

  for (const key of keys) {
    const start = fnv1a(key) % BRAND_HUES.length;
    let index = start;
    for (let probe = 0; probe < BRAND_HUES.length; probe++) {
      const candidate = (start + probe) % BRAND_HUES.length;
      if (!taken.has(candidate)) {
        index = candidate;
        break;
      }
    }
    taken.add(index);

    const hue = BRAND_HUES[index]!;
    // Saturated and mid-dark: a 8px square has to hold its color against a
    // white chip, which the muted avatar tints are too pale to do.
    accents[key] = `hsl(${hue.h}, ${Math.min(hue.s + 20, 70)}%, 45%)`;
  }

  return accents;
}
