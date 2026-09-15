import { tone } from "./tone";

/**
 * The badge colours that this app had before the port to stock shadcn.
 *
 * A badge is a tone plus the casing it draws its label in. The colours
 * themselves, and the contrast they have to clear, live in `tone.ts`. Every
 * tone gives a full set, so `src/components/ui/badge.tsx` stays stock. Put a
 * tone in `className` next to `variant="outline"`. The tone then wins through
 * tailwind-merge.
 */
export const badgeTone = {
  neutral: `${tone.neutral} uppercase`,
  success: `${tone.success} uppercase`,
  warning: `${tone.warning} uppercase`,
  destructive: `${tone.destructive} uppercase`,
} as const;
