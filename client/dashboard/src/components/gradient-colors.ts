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

export function getGradientColors(label: string): {
  from: string;
  to: string;
  angle: number;
} {
  // FNV-1a hash for good distribution across short labels.
  const fnv1a = (str: string) => {
    let hash = 2166136261;
    for (let i = 0; i < str.length; i++) {
      hash ^= str.charCodeAt(i);
      hash +=
        (hash << 1) + (hash << 4) + (hash << 7) + (hash << 8) + (hash << 24);
    }
    return hash >>> 0;
  };

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
