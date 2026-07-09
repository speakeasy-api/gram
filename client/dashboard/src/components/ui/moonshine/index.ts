/*
 * Vendored subset of the moonshine design system (v1.43.1).
 *
 * Temporary compatibility barrel: app code that previously imported the
 * external package now imports from "@/components/ui/moonshine" with an
 * identical API. Components that lost the design-system decision
 * (Dialog, Modal, Card, Icon, Tooltip, Skeleton, Separator) are vendored
 * only so existing callsites keep working; their callsites migrate to the
 * winning implementations in the consolidation phase, after which they and
 * this barrel are deleted.
 *
 * Dropped relative to upstream (unused by the dashboard): AppLayout,
 * WorkspaceSelector, LoggedInUserMenu, Subnav, PageHeader, DragNDrop,
 * CodePlayground, CodeEditorLayout, Wizard, AIChat*, PromptInput,
 * LanguageIndicator, TargetLanguageIcon, PullRequestLink, CLIWizard,
 * GradientCircle, Logo, ActionBar, ExternalPill, HighlightedText,
 * Container, Facepile, Combobox, Command, Select, Popover, Switch, Tabs,
 * SegmentedButton, UserAvatar, ContextDropdown, Heading/Text (internal
 * only, not re-exported).
 */

export { Grid } from "@/components/ui/moonshine/components/Grid";
export { Stack } from "@/components/ui/moonshine/components/Stack";
export { Button } from "@/components/ui/moonshine/components/Button";
export { Card } from "@/components/ui/moonshine/components/Card";
export {
  Icon,
  type IconProps,
} from "@/components/ui/moonshine/components/Icon";
export { type IconName } from "@/components/ui/moonshine/components/Icon/names";
export { Separator } from "@/components/ui/moonshine/components/Separator";
export { Skeleton } from "@/components/ui/moonshine/components/Skeleton";
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
export {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
  TooltipProvider,
  TooltipPortal,
} from "@/components/ui/moonshine/components/Tooltip";
export { Link } from "@/components/ui/moonshine/components/Link";
export { Dialog } from "@/components/ui/moonshine/components/Dialog";
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
export { ModalProvider } from "@/components/ui/moonshine/context/ModalContext";
export { Modal } from "@/components/ui/moonshine/components/Modal";
export { ThemeSwitcher } from "@/components/ui/moonshine/components/ThemeSwitcher";
export { cn } from "@/components/ui/moonshine/lib/utils";
/** @public — kept for upcoming design-system adoption (Kbd/stories) */
export { Timeline } from "@/components/ui/moonshine/components/Timeline";
