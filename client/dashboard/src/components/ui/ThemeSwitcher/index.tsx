"use client";

import { ReactNode, useId } from "react";
import { cn } from "@/lib/utils";
import { Moon, Sun } from "lucide-react";
import { useIsMounted } from "@/components/ui/hooks/useIsMounted";
import { useConfig } from "@/components/ui/hooks/useConfig";
import { Theme } from "@/components/ui/context/theme";

const THEMES: { key: Theme; icon: ReactNode }[] = [
  { key: "light", icon: <Sun /> },
  { key: "dark", icon: <Moon /> },
];

export interface ThemeSwitcherProps {
  onThemeSwitch?: (theme: string) => void;
  className?: string;
  orientation?: "horizontal" | "vertical";
}

export function ThemeSwitcher({
  className,
  onThemeSwitch,
  orientation = "horizontal",
}: ThemeSwitcherProps): React.JSX.Element | null {
  const isMounted = useIsMounted();
  const { theme, setTheme } = useConfig();
  const rId = useId();

  const isVertical = orientation === "vertical";
  const segmentSizeRem = 2.125;
  const placeholderStyle = isVertical
    ? {
        width: `calc(${segmentSizeRem}rem + 2px)`,
        height: `calc(${segmentSizeRem}rem * ${THEMES.length} + 2px)`,
      }
    : {
        width: `calc(${segmentSizeRem}rem * ${THEMES.length} + 2px)`,
        height: `calc(${segmentSizeRem}rem + 2px)`,
      };

  if (!isMounted) return <div style={placeholderStyle} />;

  return (
    <div className="border-border bg-background h-fit w-fit border p-0">
      <fieldset
        className={cn(
          "group m-0 flex",
          isVertical ? "flex-col items-stretch" : "flex-row items-center",
          className,
        )}
      >
        <legend className="sr-only">Select a display theme:</legend>
        {THEMES.map(({ key, icon }) => {
          const checked = key === theme;
          const id = `theme-toggle-${key}-${rId}`;
          return (
            <span key={key} className="h-full">
              <input
                tabIndex={checked ? -1 : 0}
                className="peer absolute appearance-none outline-0"
                aria-label={key}
                name={`theme-toggle-${rId}`}
                checked={checked}
                id={id}
                onChange={(): void => {
                  setTheme(key);
                  onThemeSwitch?.(key);
                }}
                type="radio"
                value={key}
                suppressHydrationWarning
              />
              <label
                className={cn(
                  // Base
                  "text-muted-foreground relative flex size-[2.125rem] cursor-pointer items-center justify-center transition-colors duration-200",
                  // Checked
                  "peer-checked:bg-primary peer-checked:text-primary-foreground peer-checked:cursor-default",
                  // Hover
                  "peer-interact:text-foreground peer-checked:peer-interact:text-primary-foreground",
                  // Focus
                  "peer-checked:!inset-ring-0 peer-focus-visible:inset-ring-2 peer-focus-visible:ring-foreground",
                  // Icon
                  "relative z-[1] [&_svg]:size-4 [&_svg]:text-current",
                )}
                htmlFor={id}
              >
                <span className="sr-only">{key}</span>
                {icon}
              </label>
            </span>
          );
        })}
      </fieldset>
    </div>
  );
}
