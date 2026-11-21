"use client";

import * as lucide from "lucide-react";
import type { LucideIcon, LucideProps } from "lucide-react";
import type { ResponsiveValue, Size } from "@/components/ui/lib/types";
import useTailwindBreakpoint from "@/components/ui/hooks/useTailwindBreakpoint";
import { resolveSizeForBreakpoint } from "@/components/ui/lib/responsiveUtils";

import type { IconName } from "./names";

type SvgProps = Pick<
  LucideProps,
  | "stroke"
  | "strokeWidth"
  | "strokeOpacity"
  | "size"
  | "viewBox"
  | "fill"
  | "fillOpacity"
  | "className"
>;

const sizeMap: Record<Size, number> = {
  small: 16,
  medium: 24,
  large: 32,
  xl: 48,
  "2xl": 64,
};

export interface IconProps extends Omit<SvgProps, "size"> {
  name: IconName;
  size?: ResponsiveValue<Size>;
}

// Icons are looked up on the lucide-react namespace, not through
// lucide-react/dynamicIconImports. The production build already puts every
// lucide module in one chunk, so lazy per-icon imports saved nothing there,
// while in dev they made Vite pre-bundle each icon as its own file: about
// 1,900 extra requests on every page load, each one a module the dev server
// keeps in memory.
const icons: Readonly<Record<string, unknown>> = lucide;

// "arrow-down-0-1" -> "ArrowDown01", the name lucide-react exports it under.
function exportName(name: string): string {
  return name.replace(/(?:^|-)([a-z0-9])/g, (_match, char: string) =>
    char.toUpperCase(),
  );
}

function getIcon(name: string): LucideIcon | undefined {
  // Safe: every key of dynamicIconImports, which IconName is derived from, is
  // exported from lucide-react as an icon component (see index.test.ts).
  return icons[exportName(name)] as LucideIcon | undefined;
}

export function Icon({
  name,
  size = "small",
  ...props
}: IconProps): React.JSX.Element | null {
  const LucideIcon = getIcon(name);
  const breakpoint = useTailwindBreakpoint();
  const resolvedSize = resolveSizeForBreakpoint(breakpoint, size, "small");
  const sizeNumber = sizeMap[resolvedSize];

  if (!LucideIcon) return null;

  return <LucideIcon {...props} size={sizeNumber} />;
}
