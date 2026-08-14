import { describe, expect, it } from "vitest";

import { badgeTone } from "./badgeTone";

// A literal list, not Object.keys: dropping a tone must fail these tests rather
// than shrink what they check.
const TONES = ["neutral", "success", "warning", "destructive"] as const;

// Badge text is 12px at weight 500, which is not WCAG large text, so the AA
// threshold is the 4.5:1 one rather than 3:1.
const AA_NORMAL_TEXT = 4.5;

type Rgb = [number, number, number];

// The tones are Tailwind arbitrary values, so the colours are in the class
// string and nowhere else. Reading them back out is the only way a test can
// measure what actually ships. Tailwind writes a space as an underscore.
function hslIn(classes: string, utility: string): Rgb {
  const found = new RegExp(
    `(?:^|\\s)${utility}-\\[hsl\\((\\d+)_(\\d+)%_(\\d+)%\\)\\]`,
  ).exec(classes);
  if (!found) throw new Error(`no ${utility} colour in "${classes}"`);
  const [, h, s, l] = found;
  return hslToRgb(Number(h), Number(s) / 100, Number(l) / 100);
}

function hslToRgb(h: number, s: number, l: number): Rgb {
  const c = (1 - Math.abs(2 * l - 1)) * s;
  const x = c * (1 - Math.abs(((h / 60) % 2) - 1));
  const m = l - c / 2;
  const sector = Math.floor(h / 60) % 6;
  const rgb: Rgb[] = [
    [c, x, 0],
    [x, c, 0],
    [0, c, x],
    [0, x, c],
    [x, 0, c],
    [c, 0, x],
  ];
  const channels = rgb[sector] ?? rgb[0];
  if (!channels) throw new Error(`unreachable sector ${sector}`);
  return channels.map((v) => Math.round((v + m) * 255)) as Rgb;
}

// WCAG 2.x relative luminance and contrast.
function luminance([r, g, b]: Rgb): number {
  const [rl, gl, bl] = [r, g, b].map((raw) => {
    const v = raw / 255;
    return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
  }) as Rgb;
  return 0.2126 * rl + 0.7152 * gl + 0.0722 * bl;
}

function contrast(a: Rgb, b: Rgb): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x) as [
    number,
    number,
  ];
  return (hi + 0.05) / (lo + 0.05);
}

describe("badgeTone", () => {
  it.each(TONES)("%s is a non-empty class string", (tone) => {
    expect(badgeTone[tone].trim()).not.toBe("");
  });

  // Both schemes, because a tone that reads in one is not a tone that reads.
  // Every consumer of `badgeTone` inherits whatever this allows, which is why
  // the check lives beside the tones rather than beside any one badge.
  it.each(TONES)("%s clears AA against its own light background", (tone) => {
    const classes = badgeTone[tone];
    expect(
      contrast(hslIn(classes, "text"), hslIn(classes, "bg")),
    ).toBeGreaterThanOrEqual(AA_NORMAL_TEXT);
  });

  it.each(TONES)("%s clears AA against its own dark background", (tone) => {
    const classes = badgeTone[tone];
    expect(
      contrast(hslIn(classes, "dark:text"), hslIn(classes, "dark:bg")),
    ).toBeGreaterThanOrEqual(AA_NORMAL_TEXT);
  });

  // The conversion and the formula are what every assertion above rests on, so
  // they are pinned against a hand-worked case rather than trusted.
  it("measures a known pair the way a browser does", () => {
    expect(hslToRgb(21, 0.64, 0.43)).toEqual([180, 89, 39]);
    expect(hslToRgb(29, 1, 0.95)).toEqual([255, 242, 229]);
    // The ratio that failed review, to two decimal places.
    expect(contrast([180, 89, 39], [255, 242, 229])).toBeCloseTo(4.34, 2);
    // Black on white is the definition's own upper bound.
    expect(contrast([0, 0, 0], [255, 255, 255])).toBeCloseTo(21, 5);
  });
});
