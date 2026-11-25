import { useContext } from "react";
import { ConfigContext } from "@/components/ui/context/config";

/**
 * Whether the resolved theme is dark.
 *
 * Read OPTIONALLY (rather than via useConfig, which throws without a
 * ConfigProvider) so bare test and storybook mounts resolve to the light
 * theme instead of failing.
 *
 * This is the single source for the question. Anything painting a colour it
 * cannot express as a CSS token — a canvas chart, an inline style — has to
 * resolve the theme in JS, and two copies of this hook would let those
 * surfaces drift apart the next time the theme context changes.
 */
export function useIsDarkTheme(): boolean {
  return useContext(ConfigContext)?.theme === "dark";
}
