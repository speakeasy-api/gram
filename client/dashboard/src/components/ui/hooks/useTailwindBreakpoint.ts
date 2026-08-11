import { useSyncExternalStore } from "react";
import { Breakpoint } from "@/components/ui/lib/types";
import debounce from "@/components/ui/lib/debounce";

// Matches Tailwind's default breakpoints.
const breakpointValues = {
  xs: 0, // Default/mobile first
  sm: 640, // @media (min-width: 640px)
  md: 768, // @media (min-width: 768px)
  lg: 1024, // @media (min-width: 1024px)
  xl: 1280, // @media (min-width: 1280px)
  "2xl": 1536, // @media (min-width: 1536px)
} as const;

const descending = Object.entries(breakpointValues).sort(
  (a, b) => b[1] - a[1],
) as [Breakpoint, number][];

const getBreakpoint = (width: number): Breakpoint => {
  for (const [key, minWidth] of descending) {
    if (width >= minWidth) return key;
  }
  return "xs";
};

// One resize listener for the whole app rather than one per subscriber. Icon
// alone mounts in the hundreds on a busy page, and a listener each turned every
// resize into O(icons) redundant breakpoint recalculations.
const listeners = new Set<() => void>();
let current: Breakpoint =
  typeof window === "undefined" ? "xs" : getBreakpoint(window.innerWidth);

const onResize = debounce(() => {
  const next = getBreakpoint(window.innerWidth);
  if (next === current) return;
  current = next;
  for (const listener of listeners) listener();
}, 100);

function subscribe(listener: () => void): () => void {
  if (listeners.size === 0) window.addEventListener("resize", onResize);
  listeners.add(listener);

  return () => {
    listeners.delete(listener);
    if (listeners.size === 0) window.removeEventListener("resize", onResize);
  };
}

const getSnapshot = (): Breakpoint => current;
const getServerSnapshot = (): Breakpoint => "xs";

const useTailwindBreakpoint = (): Breakpoint =>
  useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);

export default useTailwindBreakpoint;
