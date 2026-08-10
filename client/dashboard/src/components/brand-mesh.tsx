import type { ReactElement } from "react";

/**
 * Brand-rainbow mesh treatment shared by the project home assistant card and
 * the /chat landing: the full tech-color rainbow (--gradient-brand-primary in
 * base.css) run along a diagonal, then masked so it only breathes in from the
 * top-right corner — an accent on the surface's edge, not a background. The
 * host surface itself stays a neutral theme-following gradient.
 */
const RAINBOW_EDGE_GRADIENT =
  "linear-gradient(230deg, #99c2ff 0%, #2874d7 7%, #00143d 14%, #002414 21%, #59824f 28%, #d3dd92 35%, #fb8841 42%, #c83228 49%, #330f1f 56%, rgba(51,15,31,0) 70%)";

/** Confines the rainbow to the top-right corner, dissolving inward. */
const RAINBOW_EDGE_MASK =
  "radial-gradient(85% 120% at 100% 0%, #000 0%, rgba(0,0,0,0.6) 40%, transparent 72%)";

/**
 * Film grain laid over the mesh — an inline SVG turbulence tile, so no
 * network request and no image asset. Blended and heavily faded; texture only.
 */
const GRAIN_TEXTURE_URL =
  "url(\"data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='140' height='140'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.65' numOctaves='3'/%3E%3C/filter%3E%3Crect width='140' height='140' filter='url(%23n)'/%3E%3C/svg%3E\")";

/**
 * The two decorative layers of the treatment. Render inside a host that is
 * `relative isolate` and paints its own neutral surface (typically
 * `bg-gradient-to-br from-card to-background`); the layers sit at -z-10 so
 * content stays above them, and clip themselves so the host doesn't need
 * `overflow-hidden` (which would clip popovers like the composer's slash
 * menu).
 */
export function BrandMeshLayers(): ReactElement {
  return (
    <>
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 -z-10 overflow-hidden opacity-60 saturate-[0.65]"
        style={{
          background: RAINBOW_EDGE_GRADIENT,
          maskImage: RAINBOW_EDGE_MASK,
          WebkitMaskImage: RAINBOW_EDGE_MASK,
        }}
      />
      <div
        aria-hidden="true"
        // Multiply beds the grain into a light surface; in dark mode
        // multiply is a no-op on near-black, so switch to screen.
        className="pointer-events-none absolute inset-0 -z-10 opacity-[0.45] mix-blend-multiply grayscale dark:opacity-[0.5] dark:mix-blend-screen"
        style={{ backgroundImage: GRAIN_TEXTURE_URL }}
      />
    </>
  );
}
