/*
 * The vendored half of the design system: components that originated in the
 * (now-retired) moonshine package and won the consolidation. Everything the
 * app imports from here is a live, canonical component — exactly one
 * implementation per pattern.
 *
 * The rest of the catalog lives alongside as `@/components/ui/<name>`
 * (dialog, tooltip, select, tabs, card, heading, skeleton, separator, …).
 * See the `gram-design-system` skill for the full catalog.
 *
 * Icons: import statically from `lucide-react`; data-driven icon names go
 * through `@/components/ui/dynamic-icon`.
 */

export { Grid } from "@/components/ui/moonshine/components/Grid";
export { Stack } from "@/components/ui/moonshine/components/Stack";
export { Button } from "@/components/ui/moonshine/components/Button";
export {
  Badge,
  type BadgeProps,
} from "@/components/ui/moonshine/components/Badge";
/** @public — kept for upcoming design-system adoption (Kbd/stories) */
export {
  Score,
  type ScoreValue,
} from "@/components/ui/moonshine/components/Score";
export { CodeSnippet } from "@/components/ui/moonshine/components/CodeSnippet";
/** @public — theme provider is part of the public design-system API */
export {
  MoonshineConfigProvider as ThemeProvider,
  type MoonshineConfigProviderProps as ThemeProviderProps,
} from "@/components/ui/moonshine/context/ConfigContext";
export { useConfig as useTheme } from "@/components/ui/moonshine/hooks/useConfig";
export { type Theme } from "@/components/ui/moonshine/hooks/useTheme";
export { Alert } from "@/components/ui/moonshine/components/Alert";
export {
  Table,
  type TableProps,
  type Column,
  type SortDescriptor,
} from "@/components/ui/moonshine/components/Table";
export { sortTableData } from "@/components/ui/moonshine/components/Table/sorting";
export { Input } from "@/components/ui/moonshine/components/Input";
export { Link } from "@/components/ui/moonshine/components/Link";
/** @public — kept for upcoming design-system adoption (Kbd/stories) */
export {
  Key,
  type KeyProps,
  KeyHint,
  type KeyHintProps,
} from "@/components/ui/moonshine/components/KeyHint";
export { ResizablePanel } from "@/components/ui/moonshine/components/ResizablePanel";
export {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuGroup,
  DropdownMenuTrigger,
  DropdownMenuSeparator,
} from "@/components/ui/moonshine/components/Dropdown";
export { ThemeSwitcher } from "@/components/ui/moonshine/components/ThemeSwitcher";
export { cn } from "@/components/ui/moonshine/lib/utils";
/** @public — kept for upcoming design-system adoption (Kbd/stories) */
export { Timeline } from "@/components/ui/moonshine/components/Timeline";
