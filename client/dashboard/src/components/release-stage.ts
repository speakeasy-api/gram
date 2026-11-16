export type ReleaseStage = "preview" | "beta";

// Editorial tag chip: square, mono uppercase, hairline border on bg-card.
// Beta keeps the brand-blue text (the historical stage color); preview uses
// warning-orange. Color lives in text + border only — no tinted washes.
export const stageTextClass: Record<ReleaseStage, string> = {
  preview: "text-default-warning border-warning-softest",
  beta: "text-default-information border-information-softest",
};

export const stageLabel: Record<ReleaseStage, string> = {
  preview: "Preview",
  beta: "Beta",
};
