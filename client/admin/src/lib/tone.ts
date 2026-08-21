/**
 * The four feedback colours, as a border, a background and a foreground for
 * each scheme.
 *
 * The values come from the moonshine feedback palette, so each tone keeps the
 * meaning it had: green for a good result, orange for attention, red for a
 * failure, and grey for a plain label. The drawing models them as `--tone-*`
 * tokens shared by anything that speaks in one, so they live here rather than
 * inside the badge: a second copy of the palette drifts.
 *
 * Colour only. `uppercase` belongs to the badge and is added there.
 *
 * The smallest text drawn in a tone is not WCAG large text, so each foreground
 * has to clear 4.5:1 against its own background in both schemes.
 * `tone.test.ts` measures that from these strings; do not add or retune a tone
 * without reading what it says.
 */
export const tone = {
  neutral:
    "border-[hsl(0_0%_92%)] bg-[hsl(0_0%_98%)] text-[hsl(0_0%_33%)] dark:border-[hsl(0_0%_20%/56%)] dark:bg-[hsl(0_0%_7%)] dark:text-[hsl(0_0%_86%)]",
  success:
    "border-[hsl(99_30%_73%)] bg-[hsl(102_35%_93%)] text-[hsl(105_24%_27%)] dark:border-[hsl(105_24%_27%)] dark:bg-[hsl(105_24%_13%)] dark:text-[hsl(99_30%_73%)]",
  // The light foreground is darkened from the moonshine original's
  // `hsl(21 64% 43%)`, which measured 4.34:1 on this background and so failed
  // AA. It is the only tone that did. The dark scheme, and the same colour
  // where it serves as the dark border, are untouched.
  warning:
    "border-[hsl(28_100%_74%)] bg-[hsl(29_100%_95%)] text-[hsl(21_70%_37%)] dark:border-[hsl(21_64%_43%)] dark:bg-[hsl(18_69%_24%)] dark:text-[hsl(28_100%_74%)]",
  destructive:
    "border-[hsl(3_78%_71%)] bg-[hsl(0_100%_97%)] text-[hsl(1_64%_32%)] dark:border-[hsl(1_64%_32%)] dark:bg-[hsl(0_62%_17%)] dark:text-[hsl(3_78%_71%)]",
} as const;
