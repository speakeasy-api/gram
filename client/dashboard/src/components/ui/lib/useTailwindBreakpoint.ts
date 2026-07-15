import { useState, useLayoutEffect } from "react";
import { Breakpoint } from "@/components/ui/lib/types.js";
import debounce from "@/components/ui/lib/debounce";

// Define breakpoints in pixels matching Tailwind's default breakpoints
const breakpointValues = {
  xs: 0, // Default/mobile first
  sm: 640, // @media (min-width: 640px)
  md: 768, // @media (min-width: 768px)
  lg: 1024, // @media (min-width: 1024px)
  xl: 1280, // @media (min-width: 1280px)
  "2xl": 1536, // @media (min-width: 1536px)
} as const;

const getBreakpoint = (width: number): Breakpoint => {
  const breakpoints = Object.entries(breakpointValues).sort(
    (a, b) => b[1] - a[1],
  );

  for (const [key, minWidth] of breakpoints) {
    if (width >= minWidth) {
      return key as Breakpoint;
    }
  }

  return "xs";
};

const useTailwindBreakpoint = (): Breakpoint => {
  const [breakpoint, setBreakpoint] = useState<Breakpoint>(
    getBreakpoint(window.innerWidth),
  );

  useLayoutEffect(() => {
    const handleResize = debounce(() => {
      const newBreakpoint = getBreakpoint(window.innerWidth);
      setBreakpoint(newBreakpoint);
    }, 100);
    window.addEventListener("resize", handleResize);
    handleResize();
    return () => window.removeEventListener("resize", handleResize);
  }, []);

  return breakpoint;
};

export default useTailwindBreakpoint;
