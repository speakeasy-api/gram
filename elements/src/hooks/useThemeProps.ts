import { useMemo } from "react";
import { useElements } from "./useElements";

/**
 * Hook to get theme-related props including dark mode class
 */
export const useThemeProps = (): {
  readonly className: string | undefined;
} => {
  const { config } = useElements();
  const colorScheme = config.theme?.colorScheme ?? "light";

  return useMemo(() => {
    const isDark =
      colorScheme === "dark" ||
      (colorScheme === "system" &&
        typeof window !== "undefined" &&
        window.matchMedia("(prefers-color-scheme: dark)").matches);

    return {
      className: isDark ? "dark" : undefined,
    } as const;
  }, [colorScheme]);
};
