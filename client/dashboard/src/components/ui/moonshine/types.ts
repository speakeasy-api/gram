// Button variants
const buttonVariants = [
  "brand",
  "primary",
  "secondary",
  "tertiary",
  "destructive-primary",
  "destructive-secondary",
] as const;
export type ButtonVariant = (typeof buttonVariants)[number];

// Button sizes
const buttonSizes = ["xs", "sm", "md", "lg"] as const;
export type ButtonSize = (typeof buttonSizes)[number];

// Button contexts
const buttonContexts = ["product", "marketing"] as const;
export type ButtonContext = (typeof buttonContexts)[number];

// Badge variants
const badgeVariants = [
  "neutral",
  "destructive",
  "information",
  "success",
  "warning",
] as const;
export type BadgeVariant = (typeof badgeVariants)[number];

// Badge sizes
const badgeSizes = ["sm", "md", "lg"] as const;
export type BadgeSize = (typeof badgeSizes)[number];

// Generic
export type Orientation = "horizontal" | "vertical";

export const sizes = ["small", "medium", "large", "xl", "2xl"] as const;
export type Size = (typeof sizes)[number];

// Breakpoints
export const breakpoints = ["xs", "sm", "md", "lg", "xl", "2xl"] as const;
export type Breakpoint = (typeof breakpoints)[number];
export type ResponsiveValue<T> = T | { [key in Breakpoint]?: T };

const tailwindScale = [
  0, 0.5, 1, 1.5, 2, 2.5, 3, 3.5, 4, 5, 6, 7, 8, 9, 10, 11, 12, 14, 16, 20, 24,
  28, 32, 36, 40, 44, 48, 52, 56, 60, 64, 72, 80, 96,
] as const;

// Gap
const gapValues = tailwindScale;
export type Gap = (typeof gapValues)[number];

// Grid Columns
const gridColumnValues = tailwindScale;
export type Columns = (typeof gridColumnValues)[number];

// Padding
const paddingValues = tailwindScale;
type PaddingValue = (typeof paddingValues)[number];

export type PaddingPerAxis = { x?: PaddingValue; y?: PaddingValue };
export type PaddingPerSides = {
  top?: PaddingValue;
  right?: PaddingValue;
  bottom?: PaddingValue;
  left?: PaddingValue;
};

export type PaddingPerSide =
  /**
   * x, y
   */
  | PaddingPerAxis
  /**
   * top, right, bottom, left
   */
  | PaddingPerSides;

export type Padding = PaddingValue | PaddingPerSide;

const supportedLanguages = [
  "typescript",
  "go",
  "java",
  "python",
  "csharp",
  "terraform",
  "unity",
  "php",
  "swift",
  "ruby",
  "postman",
  "json",
] as const;

export type SupportedLanguage = (typeof supportedLanguages)[number];

const programmingLanguages = [
  "javascript",
  "typescript",
  "python",
  "bash",
  "json",
  "go",
  "dotnet",
  "java",
] as const;
export type ProgrammingLanguage = (typeof programmingLanguages)[number];
