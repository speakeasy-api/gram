"use client";

import { lazy, Suspense } from "react";
import { LucideProps } from "lucide-react";
import dynamicIconImports from "lucide-react/dynamicIconImports";
import { ResponsiveValue, Size } from "@/components/ui/lib/types";
import useTailwindBreakpoint from "@/components/ui/lib/useTailwindBreakpoint";
import { resolveSizeForBreakpoint } from "@/components/ui/lib/responsiveUtils";

import customDynamicIconImports from "./customIcons";
import { IconName } from "./names";

// TODO: Use skeleton
function Skeleton() {
  return <div />;
}

// Skeleton fallback
const fallback = <Skeleton />;

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

const iconCache = new Map<
  string,
  React.LazyExoticComponent<React.ComponentType<LucideProps>>
>();

function tryGetIcon(
  name: string,
): React.LazyExoticComponent<React.ComponentType<LucideProps>> {
  if (iconCache.has(name)) {
    return iconCache.get(name) as React.LazyExoticComponent<
      React.ComponentType<LucideProps>
    >;
  }

  const LucideIcon = customDynamicIconImports[
    name as keyof typeof customDynamicIconImports
  ]
    ? lazy(
        customDynamicIconImports[name as keyof typeof customDynamicIconImports],
      )
    : lazy(dynamicIconImports[name as keyof typeof dynamicIconImports]);

  iconCache.set(name, LucideIcon);
  return LucideIcon;
}

export function Icon({
  name,
  size = "small",
  ...props
}: IconProps): React.JSX.Element {
  const LucideIcon = tryGetIcon(name);
  const breakpoint = useTailwindBreakpoint();
  const resolvedSize = resolveSizeForBreakpoint(breakpoint, size, "small");
  const sizeNumber = sizeMap[resolvedSize];

  return (
    <Suspense fallback={fallback}>
      <LucideIcon {...props} size={sizeNumber} />
    </Suspense>
  );
}
