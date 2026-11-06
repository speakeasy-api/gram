import logoRiv from "@/assets/speakeasy-logo-loop.riv?url";
import { useIsDarkTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";
import riveWasm from "@rive-app/canvas/rive.wasm?url";
import riveFallbackWasm from "@rive-app/canvas/rive_fallback.wasm?url";
import { Rive, RuntimeLoader, useRive } from "@rive-app/react-canvas";
import { useReducedMotion } from "motion/react";

// Serve the Rive runtime from our own origin. By default it is fetched from
// unpkg, with jsdelivr as the fallback.
RuntimeLoader.setWasmUrl(riveWasm);
RuntimeLoader.setWasmFallbackUrl(riveFallbackWasm);
// The logo file exposes its loop only as named timeline animations, which
// this runtime flags as deprecated on every play. Silence those warnings.
Rive.suppressDeprecationWarnings = [
  "animations-param",
  "animation-names",
  "names-array",
];

/**
 * The animated Speakeasy logo: the one loading indicator for a full page.
 * Sections use skeletons and inline controls use `Spinner`.
 */
export function LogoLoader({ className }: { className?: string }): JSX.Element {
  const isDark = useIsDarkTheme();
  return (
    <div
      role="status"
      aria-label="Loading"
      className={cn("size-16 shrink-0", className)}
    >
      {/* The animation is picked when the file loads, so a theme change
          mounts a fresh canvas. */}
      <LogoCanvas key={isDark ? "dark" : "light"} isDark={isDark} />
    </div>
  );
}

function LogoCanvas({ isDark }: { isDark: boolean }): JSX.Element {
  const reduceMotion = useReducedMotion();
  const theme = isDark ? "dark" : "light";
  const { rive, RiveComponent } = useRive({
    src: logoRiv,
    animations: `${reduceMotion ? "idle" : "loop"}-${theme}`,
    autoplay: true,
  });
  return (
    <RiveComponent
      aria-hidden
      className={cn(
        "size-full transition-opacity duration-300",
        rive ? "opacity-100" : "opacity-0",
      )}
    />
  );
}

/**
 * Fills its container with the logo loader. Use `fullScreen` to cover the
 * viewport, for example while the app boots.
 *
 * The loader fades in after a short delay, so a fast load does not flash it.
 */
export function SplashScreen({
  fullScreen = false,
  className,
}: {
  fullScreen?: boolean;
  className?: string;
}): JSX.Element {
  return (
    <div
      className={cn(
        "animate-in fade-in fill-mode-both flex items-center justify-center delay-200 duration-300",
        fullScreen
          ? "bg-background fixed inset-0 z-50"
          : "min-h-[50vh] w-full flex-1",
        className,
      )}
    >
      <LogoLoader />
    </div>
  );
}
