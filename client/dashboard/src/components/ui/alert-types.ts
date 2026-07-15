export const variants = [
  "default",
  "success",
  "error",
  "warning",
  "info",
  "feature",
] as const;
export type Variant = (typeof variants)[number];

const modifiers = ["inline"] as const;
export type Modifier = (typeof modifiers)[number];
