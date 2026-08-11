import type { ReactElement } from "react";

/** Base host treatment shared by every brand-mesh surface. */
export const BRAND_MESH_SURFACE_CLASS =
  "from-card to-background relative isolate bg-gradient-to-br";

/**
 * Brand-rainbow mesh treatment shared by the project home assistant card and
 * the /chat landing: the full tech-color rainbow (--gradient-brand-primary in
 * base.css) run along a diagonal, then masked so it only breathes in from an
 * edge — an accent on the surface, not a background. The host surface itself
 * stays a neutral theme-following gradient.
 */
const RAINBOW_EDGE_STOPS =
  "#99c2ff 0%, #2874d7 7%, #00143d 14%, #002414 21%, #59824f 28%, #d3dd92 35%, #fb8841 42%, #c83228 49%, #330f1f 56%, rgba(51,15,31,0) 70%";

const MESH_VARIATIONS = [
  { angle: 230, focalPoint: "100% 0%" },
  { angle: 130, focalPoint: "0% 0%" },
  { angle: 310, focalPoint: "100% 100%" },
  { angle: 50, focalPoint: "0% 100%" },
  { angle: 255, focalPoint: "100% 35%" },
  { angle: 105, focalPoint: "0% 35%" },
  { angle: 285, focalPoint: "80% 100%" },
  { angle: 75, focalPoint: "20% 100%" },
] as const;

const SEEDED_BRAND_COLORS = [
  "var(--color-brand-red-200)",
  "var(--color-brand-red-400)",
  "var(--color-brand-red-600)",
  "var(--color-brand-green-200)",
  "var(--color-brand-green-400)",
  "var(--color-brand-green-600)",
  "var(--color-brand-blue-200)",
  "var(--color-brand-blue-400)",
  "var(--color-brand-blue-600)",
] as const;

function hashSeed(seed: string): number {
  let hash = 2166136261;
  for (let index = 0; index < seed.length; index++) {
    hash ^= seed.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  return hash >>> 0;
}

function meshTreatment(seed: string | undefined) {
  if (!seed) {
    const variation = MESH_VARIATIONS[0];
    return {
      variation,
      gradient: `linear-gradient(${variation.angle}deg, ${RAINBOW_EDGE_STOPS})`,
    };
  }

  const hash = hashSeed(seed);
  const color =
    SEEDED_BRAND_COLORS[hash % SEEDED_BRAND_COLORS.length] ??
    SEEDED_BRAND_COLORS[0];
  const variation =
    MESH_VARIATIONS[
      Math.floor(hash / SEEDED_BRAND_COLORS.length) % MESH_VARIATIONS.length
    ] ?? MESH_VARIATIONS[0];

  return {
    variation,
    gradient: `linear-gradient(${variation.angle}deg, ${color} 0%, ${color} 48%, transparent 78%)`,
  };
}

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
 * menu). Passing a seed replaces the rainbow with one stable brand color and
 * edge composition, allowing repeated cards to keep distinct identities.
 */
export function BrandMeshLayers({ seed }: { seed?: string }): ReactElement {
  const { variation, gradient } = meshTreatment(seed);
  const mask = `radial-gradient(85% 120% at ${variation.focalPoint}, #000 0%, rgba(0,0,0,0.6) 40%, transparent 72%)`;

  return (
    <>
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 -z-10 overflow-hidden opacity-60 saturate-[0.65]"
        style={{
          background: gradient,
          maskImage: mask,
          WebkitMaskImage: mask,
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
