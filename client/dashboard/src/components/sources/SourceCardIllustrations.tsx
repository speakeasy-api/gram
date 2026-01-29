import { cn } from "@/lib/utils";

// Import pattern images
import blueCascade from "@/assets/patterns/blue-cascade.png";
import blueCascadeAlt from "@/assets/patterns/blue-cascade-alt.png";
import blueHorizontal from "@/assets/patterns/blue-horizontal.png";
import blueSlot from "@/assets/patterns/blue-slot.png";
import blueTartan from "@/assets/patterns/blue-tartan.png";
import redCascade from "@/assets/patterns/red-cascade.png";
import redCascadeAlt from "@/assets/patterns/red-cascade-alt.png";
import redHorizontal from "@/assets/patterns/red-horizontal.png";
import redSlot from "@/assets/patterns/red-slot.png";
import redTartan from "@/assets/patterns/red-tartan.png";
import greenCascade from "@/assets/patterns/green-cascade.png";
import greenVertical from "@/assets/patterns/green-vertical.png";
import greenHorizontal from "@/assets/patterns/green-horizontal.png";
import greenSlot from "@/assets/patterns/green-slot.png";
import greenTartan from "@/assets/patterns/green-tartan.png";

interface IllustrationProps {
  className?: string;
}

// Generate deterministic hash from string
function hashString(str: string): number {
  let hash = 0;
  for (let i = 0; i < str.length; i++) {
    hash = str.charCodeAt(i) + ((hash << 5) - hash);
  }
  return Math.abs(hash);
}

// Seeded random number generator for deterministic "randomness"
function seededRandom(seed: number): () => number {
  return () => {
    seed = (seed * 1103515245 + 12345) & 0x7fffffff;
    return seed / 0x7fffffff;
  };
}

// Pattern images organized by color
const PATTERN_IMAGES = [
  // Blue patterns
  [blueCascade, blueCascadeAlt, blueHorizontal, blueSlot, blueTartan],
  // Red patterns
  [redCascade, redCascadeAlt, redHorizontal, redSlot, redTartan],
  // Green patterns
  [greenCascade, greenVertical, greenHorizontal, greenSlot, greenTartan],
];

/**
 * MCP card illustration using pattern images
 * Deterministically selects a pattern based on the toolset slug
 */
export function MCPPatternIllustration({
  className,
  toolsetSlug,
}: IllustrationProps & { toolsetSlug: string }) {
  const seed = hashString(toolsetSlug);
  const random = seededRandom(seed);

  const colorIndex = Math.floor(random() * PATTERN_IMAGES.length);
  const patternIndex = Math.floor(random() * PATTERN_IMAGES[colorIndex].length);
  const patternImage = PATTERN_IMAGES[colorIndex][patternIndex];

  return (
    <img
      src={patternImage}
      alt=""
      className={cn("w-full h-full object-fill", className)}
      aria-hidden="true"
    />
  );
}

/**
 * Large hero illustration for MCP details page
 * Uses the same pattern as the card at full saturation
 */
export function MCPHeroIllustration({
  className,
  toolsetSlug,
}: IllustrationProps & { toolsetSlug: string }) {
  const seed = hashString(toolsetSlug);
  const random = seededRandom(seed);

  const colorIndex = Math.floor(random() * PATTERN_IMAGES.length);
  const patternIndex = Math.floor(random() * PATTERN_IMAGES[colorIndex].length);
  const patternImage = PATTERN_IMAGES[colorIndex][patternIndex];

  return (
    <img
      src={patternImage}
      alt=""
      className={cn("w-full h-full object-fill", className)}
      aria-hidden="true"
    />
  );
}

/**
 * Illustration for external MCP servers
 * Shows the server logo on a pattern background if available,
 * otherwise uses the pattern alone
 */
export function ExternalMCPIllustration({
  className,
  logoUrl,
  name,
  slug,
}: IllustrationProps & { logoUrl?: string; name?: string; slug: string }) {
  if (logoUrl) {
    return (
      <div className={cn("w-full h-full relative", className)}>
        {/* Pattern background */}
        <MCPPatternIllustration
          toolsetSlug={slug}
          className="saturate-[.3] group-hover:saturate-100 transition-all duration-300"
        />
        {/* Logo overlay */}
        <div className="absolute inset-0 flex items-center justify-center">
          <div className="bg-background/90 backdrop-blur-sm rounded-lg p-3 shadow-lg">
            <img
              src={logoUrl}
              alt={name || "MCP Server"}
              className="w-12 h-12 object-contain"
            />
          </div>
        </div>
      </div>
    );
  }

  // Fallback: just use the pattern illustration
  return (
    <MCPPatternIllustration
      toolsetSlug={slug}
      className="saturate-[.3] group-hover:saturate-100 transition-all duration-300"
    />
  );
}
