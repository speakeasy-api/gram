import { scoreToRating, type SeverityRating } from "./risk-utils";

// Signal severity palette: band → text / row-edge / swatch classes. Colors are
// token-derived: brand red hsl(4,67%,47%) (--color-brand-red-500) is reserved
// for critical, the feedback-orange ramp covers high/medium, low stays neutral
// ink.
export const SEVERITY_TEXT: Record<SeverityRating, string> = {
  critical: "text-[var(--color-brand-red-500)]",
  high: "text-[var(--color-feedback-orange-600)]",
  medium: "text-[var(--color-feedback-orange-400)]",
  low: "text-foreground",
};

export const SEVERITY_EDGE: Record<SeverityRating, string> = {
  critical: "border-l-[var(--color-brand-red-500)]",
  high: "border-l-[var(--color-feedback-orange-600)]",
  medium: "border-l-[var(--color-feedback-orange-400)]",
  low: "border-l-border",
};

export const SEVERITY_SWATCH: Record<SeverityRating, string> = {
  critical: "bg-[var(--color-brand-red-500)]",
  high: "bg-[var(--color-feedback-orange-600)]",
  medium: "bg-[var(--color-feedback-orange-400)]",
  low: "bg-foreground",
};

// Ratings key off the rounded value we display, so a score sitting just below
// a band boundary (e.g. 3.96 → shown as "4.0") never renders in a color that
// disagrees with the band its displayed value falls in.
export function displayedScoreRating(score: number): {
  displayed: number;
  rating: SeverityRating;
} {
  const displayed = Math.round(score * 10) / 10;
  return { displayed, rating: scoreToRating(displayed) };
}
