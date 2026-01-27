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
export function MCPRobotIllustration({
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
      className={cn("w-full h-full object-cover", className)}
      aria-hidden="true"
    />
  );
}

/**
 * Large hero illustration for MCP details page
 * Uses the same pattern as the card at full saturation
 */
export function MCPRobotHeroIllustration({
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
      className={cn("w-full h-full object-cover", className)}
      aria-hidden="true"
    />
  );
}

/**
 * Skeleton illustration of an OpenAPI document
 * Shows parsed endpoints extracted from the spec
 */
export function OpenAPIIllustration({ className }: IllustrationProps) {
  return (
    <svg
      viewBox="0 0 280 120"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      preserveAspectRatio="xMidYMid slice"
      className={cn("w-full h-full", className)}
      aria-hidden="true"
    >
      {/* Background */}
      <rect width="280" height="120" className="fill-slate-500/[0.03]" />

      {/* Document header area */}
      <rect
        x="16"
        y="10"
        width="248"
        height="24"
        rx="3"
        className="fill-slate-500/[0.05]"
      />
      <rect
        x="24"
        y="16"
        width="60"
        height="5"
        rx="1"
        className="fill-slate-500/15"
      />
      <rect
        x="24"
        y="24"
        width="100"
        height="3"
        rx="1"
        className="fill-slate-500/8"
      />
      <text x="240" y="24" className="fill-slate-400/30 text-[7px] font-mono">
        v1.0
      </text>

      {/* Endpoints list */}
      {/* GET endpoint */}
      <rect
        x="16"
        y="42"
        width="130"
        height="18"
        rx="2"
        className="fill-slate-500/[0.04]"
      />
      <rect
        x="22"
        y="47"
        width="24"
        height="10"
        rx="2"
        className="fill-emerald-500/20"
      />
      <text
        x="26"
        y="55"
        className="fill-emerald-600/50 dark:fill-emerald-400/50 text-[6px] font-mono font-medium"
      >
        GET
      </text>
      <text x="52" y="54" className="fill-slate-500/40 text-[7px] font-mono">
        /users
      </text>

      {/* POST endpoint */}
      <rect
        x="16"
        y="64"
        width="130"
        height="18"
        rx="2"
        className="fill-slate-500/[0.04]"
      />
      <rect
        x="22"
        y="69"
        width="28"
        height="10"
        rx="2"
        className="fill-amber-500/20"
      />
      <text
        x="26"
        y="77"
        className="fill-amber-600/50 dark:fill-amber-400/50 text-[6px] font-mono font-medium"
      >
        POST
      </text>
      <text x="56" y="76" className="fill-slate-500/40 text-[7px] font-mono">
        /users
      </text>

      {/* DELETE endpoint */}
      <rect
        x="16"
        y="86"
        width="130"
        height="18"
        rx="2"
        className="fill-slate-500/[0.04]"
      />
      <rect
        x="22"
        y="91"
        width="38"
        height="10"
        rx="2"
        className="fill-red-500/20"
      />
      <text
        x="25"
        y="99"
        className="fill-red-600/50 dark:fill-red-400/50 text-[6px] font-mono font-medium"
      >
        DELETE
      </text>
      <text x="66" y="98" className="fill-slate-500/40 text-[7px] font-mono">
        /users/{"{id}"}
      </text>

      {/* Right side - schema code preview */}
      <rect
        x="156"
        y="42"
        width="108"
        height="62"
        rx="3"
        className="fill-slate-500/[0.04] stroke-slate-500/8"
        strokeWidth="1"
      />

      {/* Mini line numbers */}
      <text x="162" y="52" className="fill-slate-400/25 text-[6px] font-mono">
        1
      </text>
      <text x="162" y="60" className="fill-slate-400/25 text-[6px] font-mono">
        2
      </text>
      <text x="162" y="68" className="fill-slate-400/25 text-[6px] font-mono">
        3
      </text>
      <text x="162" y="76" className="fill-slate-400/25 text-[6px] font-mono">
        4
      </text>
      <text x="162" y="84" className="fill-slate-400/25 text-[6px] font-mono">
        5
      </text>
      <text x="162" y="92" className="fill-slate-400/25 text-[6px] font-mono">
        6
      </text>
      <text x="162" y="100" className="fill-slate-400/25 text-[6px] font-mono">
        7
      </text>

      {/* Code lines */}
      <rect
        x="172"
        y="48"
        width="30"
        height="4"
        rx="1"
        className="fill-slate-500/12"
      />
      <rect
        x="172"
        y="56"
        width="50"
        height="3"
        rx="1"
        className="fill-slate-500/8"
      />
      <rect
        x="178"
        y="64"
        width="42"
        height="3"
        rx="1"
        className="fill-slate-500/8"
      />
      <rect
        x="178"
        y="72"
        width="55"
        height="3"
        rx="1"
        className="fill-slate-500/8"
      />
      <rect
        x="178"
        y="80"
        width="38"
        height="3"
        rx="1"
        className="fill-slate-500/8"
      />
      <rect
        x="172"
        y="88"
        width="45"
        height="3"
        rx="1"
        className="fill-slate-500/8"
      />
      <rect
        x="172"
        y="96"
        width="20"
        height="3"
        rx="1"
        className="fill-slate-500/8"
      />
    </svg>
  );
}

/**
 * Skeleton illustration of a TypeScript function file
 * Shows fake code structure
 */
export function FunctionIllustration({ className }: IllustrationProps) {
  return (
    <svg
      viewBox="0 0 280 120"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      preserveAspectRatio="xMidYMid slice"
      className={cn("w-full h-full", className)}
      aria-hidden="true"
    >
      {/* Background */}
      <rect width="280" height="120" className="fill-emerald-500/5" />

      {/* Import statement */}
      <rect
        x="16"
        y="12"
        width="40"
        height="6"
        rx="1"
        className="fill-purple-500/40"
      />
      <rect
        x="60"
        y="12"
        width="30"
        height="6"
        rx="1"
        className="fill-emerald-500/30"
      />
      <rect
        x="94"
        y="12"
        width="24"
        height="6"
        rx="1"
        className="fill-purple-500/40"
      />
      <rect
        x="122"
        y="12"
        width="50"
        height="6"
        rx="1"
        className="fill-amber-500/30"
      />

      {/* Function declaration */}
      <rect
        x="16"
        y="28"
        width="50"
        height="6"
        rx="1"
        className="fill-blue-500/40"
      />
      <rect
        x="70"
        y="28"
        width="60"
        height="6"
        rx="1"
        className="fill-emerald-500/40"
      />
      <rect
        x="134"
        y="28"
        width="8"
        height="6"
        rx="1"
        className="fill-emerald-500/20"
      />

      {/* Function body - indented lines */}
      <rect
        x="28"
        y="40"
        width="35"
        height="5"
        rx="1"
        className="fill-purple-500/30"
      />
      <rect
        x="67"
        y="40"
        width="80"
        height="5"
        rx="1"
        className="fill-emerald-500/20"
      />

      <rect
        x="28"
        y="50"
        width="25"
        height="5"
        rx="1"
        className="fill-blue-500/30"
      />
      <rect
        x="57"
        y="50"
        width="45"
        height="5"
        rx="1"
        className="fill-emerald-500/20"
      />
      <rect
        x="106"
        y="50"
        width="60"
        height="5"
        rx="1"
        className="fill-amber-500/20"
      />

      <rect
        x="28"
        y="60"
        width="30"
        height="5"
        rx="1"
        className="fill-purple-500/30"
      />
      <rect
        x="62"
        y="60"
        width="55"
        height="5"
        rx="1"
        className="fill-emerald-500/20"
      />

      {/* Nested block */}
      <rect
        x="40"
        y="72"
        width="20"
        height="5"
        rx="1"
        className="fill-blue-500/30"
      />
      <rect
        x="64"
        y="72"
        width="70"
        height="5"
        rx="1"
        className="fill-emerald-500/15"
      />

      <rect
        x="40"
        y="82"
        width="35"
        height="5"
        rx="1"
        className="fill-emerald-500/20"
      />
      <rect
        x="79"
        y="82"
        width="50"
        height="5"
        rx="1"
        className="fill-amber-500/20"
      />

      {/* Return statement */}
      <rect
        x="28"
        y="96"
        width="40"
        height="5"
        rx="1"
        className="fill-purple-500/40"
      />
      <rect
        x="72"
        y="96"
        width="60"
        height="5"
        rx="1"
        className="fill-emerald-500/30"
      />

      {/* Closing brace indicator */}
      <rect
        x="16"
        y="108"
        width="6"
        height="6"
        rx="1"
        className="fill-emerald-500/30"
      />
    </svg>
  );
}

/**
 * Illustration for external MCP servers
 * Shows the server logo if available, otherwise a generic server illustration
 */
export function ExternalMCPIllustration({
  className,
  logoUrl,
  name,
}: IllustrationProps & { logoUrl?: string; name?: string }) {
  if (logoUrl) {
    return (
      <div
        className={cn(
          "w-full h-full bg-violet-500/5 flex items-center justify-center",
          className,
        )}
      >
        <img
          src={logoUrl}
          alt={name || "MCP Server"}
          className="w-16 h-16 object-contain"
        />
      </div>
    );
  }

  // Fallback: generic server/connection illustration
  return (
    <svg
      viewBox="0 0 280 120"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      preserveAspectRatio="xMidYMid slice"
      className={cn("w-full h-full", className)}
      aria-hidden="true"
    >
      {/* Background */}
      <rect width="280" height="120" className="fill-violet-500/5" />

      {/* Server box left */}
      <rect
        x="30"
        y="35"
        width="50"
        height="50"
        rx="6"
        className="fill-violet-500/20 stroke-violet-500/30"
        strokeWidth="1.5"
      />
      <rect
        x="40"
        y="45"
        width="30"
        height="4"
        rx="1"
        className="fill-violet-500/40"
      />
      <rect
        x="40"
        y="53"
        width="20"
        height="4"
        rx="1"
        className="fill-violet-500/30"
      />
      <circle cx="45" cy="72" r="3" className="fill-emerald-500/50" />
      <circle cx="55" cy="72" r="3" className="fill-emerald-500/50" />
      <circle cx="65" cy="72" r="3" className="fill-violet-500/30" />

      {/* Connection lines */}
      <path
        d="M80 60 L115 60"
        className="stroke-violet-500/40"
        strokeWidth="2"
        strokeDasharray="4 2"
      />
      <circle cx="115" cy="60" r="4" className="fill-violet-500/30" />

      {/* Central hub */}
      <rect
        x="125"
        y="40"
        width="30"
        height="40"
        rx="4"
        className="fill-violet-500/25 stroke-violet-500/35"
        strokeWidth="1.5"
      />
      <rect
        x="132"
        y="48"
        width="16"
        height="3"
        rx="1"
        className="fill-violet-500/50"
      />
      <rect
        x="132"
        y="55"
        width="16"
        height="3"
        rx="1"
        className="fill-violet-500/40"
      />
      <rect
        x="132"
        y="62"
        width="16"
        height="3"
        rx="1"
        className="fill-violet-500/40"
      />
      <rect
        x="132"
        y="69"
        width="16"
        height="3"
        rx="1"
        className="fill-violet-500/30"
      />

      {/* Connection to right */}
      <circle cx="165" cy="60" r="4" className="fill-violet-500/30" />
      <path
        d="M165 60 L200 60"
        className="stroke-violet-500/40"
        strokeWidth="2"
        strokeDasharray="4 2"
      />

      {/* Server box right */}
      <rect
        x="200"
        y="35"
        width="50"
        height="50"
        rx="6"
        className="fill-violet-500/20 stroke-violet-500/30"
        strokeWidth="1.5"
      />
      <rect
        x="210"
        y="45"
        width="30"
        height="4"
        rx="1"
        className="fill-violet-500/40"
      />
      <rect
        x="210"
        y="53"
        width="25"
        height="4"
        rx="1"
        className="fill-violet-500/30"
      />
      <circle cx="215" cy="72" r="3" className="fill-emerald-500/50" />
      <circle cx="225" cy="72" r="3" className="fill-violet-500/30" />
      <circle cx="235" cy="72" r="3" className="fill-emerald-500/50" />
    </svg>
  );
}
