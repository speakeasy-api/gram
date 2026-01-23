import { cn } from "@/lib/utils";

interface IllustrationProps {
  className?: string;
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
      <rect x="16" y="10" width="248" height="24" rx="3" className="fill-slate-500/[0.05]" />
      <rect x="24" y="16" width="60" height="5" rx="1" className="fill-slate-500/15" />
      <rect x="24" y="24" width="100" height="3" rx="1" className="fill-slate-500/8" />
      <text x="240" y="24" className="fill-slate-400/30 text-[7px] font-mono">v1.0</text>

      {/* Endpoints list */}
      {/* GET endpoint */}
      <rect x="16" y="42" width="130" height="18" rx="2" className="fill-slate-500/[0.04]" />
      <rect x="22" y="47" width="24" height="10" rx="2" className="fill-emerald-500/20" />
      <text x="26" y="55" className="fill-emerald-600/50 dark:fill-emerald-400/50 text-[6px] font-mono font-medium">GET</text>
      <text x="52" y="54" className="fill-slate-500/40 text-[7px] font-mono">/users</text>

      {/* POST endpoint */}
      <rect x="16" y="64" width="130" height="18" rx="2" className="fill-slate-500/[0.04]" />
      <rect x="22" y="69" width="28" height="10" rx="2" className="fill-amber-500/20" />
      <text x="26" y="77" className="fill-amber-600/50 dark:fill-amber-400/50 text-[6px] font-mono font-medium">POST</text>
      <text x="56" y="76" className="fill-slate-500/40 text-[7px] font-mono">/users</text>

      {/* DELETE endpoint */}
      <rect x="16" y="86" width="130" height="18" rx="2" className="fill-slate-500/[0.04]" />
      <rect x="22" y="91" width="38" height="10" rx="2" className="fill-red-500/20" />
      <text x="25" y="99" className="fill-red-600/50 dark:fill-red-400/50 text-[6px] font-mono font-medium">DELETE</text>
      <text x="66" y="98" className="fill-slate-500/40 text-[7px] font-mono">/users/{"{id}"}</text>

      {/* Right side - schema code preview */}
      <rect x="156" y="42" width="108" height="62" rx="3" className="fill-slate-500/[0.04] stroke-slate-500/8" strokeWidth="1" />

      {/* Mini line numbers */}
      <text x="162" y="52" className="fill-slate-400/25 text-[6px] font-mono">1</text>
      <text x="162" y="60" className="fill-slate-400/25 text-[6px] font-mono">2</text>
      <text x="162" y="68" className="fill-slate-400/25 text-[6px] font-mono">3</text>
      <text x="162" y="76" className="fill-slate-400/25 text-[6px] font-mono">4</text>
      <text x="162" y="84" className="fill-slate-400/25 text-[6px] font-mono">5</text>
      <text x="162" y="92" className="fill-slate-400/25 text-[6px] font-mono">6</text>
      <text x="162" y="100" className="fill-slate-400/25 text-[6px] font-mono">7</text>

      {/* Code lines */}
      <rect x="172" y="48" width="30" height="4" rx="1" className="fill-slate-500/12" />
      <rect x="172" y="56" width="50" height="3" rx="1" className="fill-slate-500/8" />
      <rect x="178" y="64" width="42" height="3" rx="1" className="fill-slate-500/8" />
      <rect x="178" y="72" width="55" height="3" rx="1" className="fill-slate-500/8" />
      <rect x="178" y="80" width="38" height="3" rx="1" className="fill-slate-500/8" />
      <rect x="172" y="88" width="45" height="3" rx="1" className="fill-slate-500/8" />
      <rect x="172" y="96" width="20" height="3" rx="1" className="fill-slate-500/8" />
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
      <rect x="16" y="12" width="40" height="6" rx="1" className="fill-purple-500/40" />
      <rect x="60" y="12" width="30" height="6" rx="1" className="fill-emerald-500/30" />
      <rect x="94" y="12" width="24" height="6" rx="1" className="fill-purple-500/40" />
      <rect x="122" y="12" width="50" height="6" rx="1" className="fill-amber-500/30" />

      {/* Function declaration */}
      <rect x="16" y="28" width="50" height="6" rx="1" className="fill-blue-500/40" />
      <rect x="70" y="28" width="60" height="6" rx="1" className="fill-emerald-500/40" />
      <rect x="134" y="28" width="8" height="6" rx="1" className="fill-emerald-500/20" />

      {/* Function body - indented lines */}
      <rect x="28" y="40" width="35" height="5" rx="1" className="fill-purple-500/30" />
      <rect x="67" y="40" width="80" height="5" rx="1" className="fill-emerald-500/20" />

      <rect x="28" y="50" width="25" height="5" rx="1" className="fill-blue-500/30" />
      <rect x="57" y="50" width="45" height="5" rx="1" className="fill-emerald-500/20" />
      <rect x="106" y="50" width="60" height="5" rx="1" className="fill-amber-500/20" />

      <rect x="28" y="60" width="30" height="5" rx="1" className="fill-purple-500/30" />
      <rect x="62" y="60" width="55" height="5" rx="1" className="fill-emerald-500/20" />

      {/* Nested block */}
      <rect x="40" y="72" width="20" height="5" rx="1" className="fill-blue-500/30" />
      <rect x="64" y="72" width="70" height="5" rx="1" className="fill-emerald-500/15" />

      <rect x="40" y="82" width="35" height="5" rx="1" className="fill-emerald-500/20" />
      <rect x="79" y="82" width="50" height="5" rx="1" className="fill-amber-500/20" />

      {/* Return statement */}
      <rect x="28" y="96" width="40" height="5" rx="1" className="fill-purple-500/40" />
      <rect x="72" y="96" width="60" height="5" rx="1" className="fill-emerald-500/30" />

      {/* Closing brace indicator */}
      <rect x="16" y="108" width="6" height="6" rx="1" className="fill-emerald-500/30" />
    </svg>
  );
}


// Generate deterministic color from string
function hashStringToColor(str: string): { bg: string; accent: string; text: string } {
  let hash = 0;
  for (let i = 0; i < str.length; i++) {
    hash = str.charCodeAt(i) + ((hash << 5) - hash);
  }

  const colorPalettes = [
    { bg: "#fef3c7", accent: "#fbbf24", text: "#92400e" }, // Warm yellow
    { bg: "#dbeafe", accent: "#60a5fa", text: "#1e40af" }, // Cool blue
    { bg: "#fce7f3", accent: "#f472b6", text: "#9f1239" }, // Pink
    { bg: "#d1fae5", accent: "#34d399", text: "#065f46" }, // Mint green
    { bg: "#e0e7ff", accent: "#818cf8", text: "#3730a3" }, // Lavender
    { bg: "#fed7aa", accent: "#fb923c", text: "#9a3412" }, // Peach
    { bg: "#fae8ff", accent: "#d946ef", text: "#86198f" }, // Purple
    { bg: "#ccfbf1", accent: "#2dd4bf", text: "#115e59" }, // Teal
  ];

  return colorPalettes[Math.abs(hash) % colorPalettes.length];
}

/**
 * MCP Card illustration with repeating hand-drawn doodles
 * Parallax horizontal scroll on hover
 */
export function MCPIllustration({
  className,
  mcpUrl,
  toolsetSlug,
}: IllustrationProps & { mcpUrl: string; toolsetSlug: string }) {
  const colors = hashStringToColor(toolsetSlug);
  const displayUrl = mcpUrl.replace(/^https?:\/\//, '');

  // Individual doodle icons - small hand-drawn style
  const ServerDoodle = ({ x, y }: { x: number; y: number }) => (
    <g transform={`translate(${x}, ${y})`}>
      <rect x="0" y="0" width="18" height="24" rx="2" fill="none" stroke={colors.accent} strokeWidth="1.2" opacity="0.6" />
      <line x1="3" y1="6" x2="15" y2="6" stroke={colors.accent} strokeWidth="1" opacity="0.4" />
      <line x1="3" y1="11" x2="15" y2="11" stroke={colors.accent} strokeWidth="1" opacity="0.4" />
      <line x1="3" y1="16" x2="15" y2="16" stroke={colors.accent} strokeWidth="1" opacity="0.4" />
      <circle cx="5" cy="21" r="1.5" fill={colors.accent} opacity="0.5" />
      <circle cx="10" cy="21" r="1.5" fill={colors.accent} opacity="0.3" />
    </g>
  );

  const CloudDoodle = ({ x, y }: { x: number; y: number }) => (
    <g transform={`translate(${x}, ${y})`}>
      <path
        d="M5 18 Q0 18 0 12 Q0 6 6 6 Q6 0 14 0 Q22 0 24 6 Q30 4 32 10 Q36 10 36 14 Q36 18 32 18 Z"
        fill="none"
        stroke={colors.accent}
        strokeWidth="1.2"
        opacity="0.5"
        strokeLinecap="round"
      />
    </g>
  );

  const DatabaseDoodle = ({ x, y }: { x: number; y: number }) => (
    <g transform={`translate(${x}, ${y})`}>
      <ellipse cx="10" cy="4" rx="10" ry="4" fill="none" stroke={colors.accent} strokeWidth="1.2" opacity="0.5" />
      <path d="M0 4 L0 20 Q0 24 10 24 Q20 24 20 20 L20 4" fill="none" stroke={colors.accent} strokeWidth="1.2" opacity="0.5" />
      <ellipse cx="10" cy="20" rx="10" ry="4" fill="none" stroke={colors.accent} strokeWidth="1" opacity="0.3" />
      <ellipse cx="10" cy="12" rx="10" ry="4" fill="none" stroke={colors.accent} strokeWidth="0.8" opacity="0.2" strokeDasharray="2 2" />
    </g>
  );

  const WifiDoodle = ({ x, y }: { x: number; y: number }) => (
    <g transform={`translate(${x}, ${y})`}>
      <circle cx="10" cy="20" r="2" fill={colors.accent} opacity="0.5" />
      <path d="M4 14 Q10 8 16 14" fill="none" stroke={colors.accent} strokeWidth="1.2" opacity="0.4" strokeLinecap="round" />
      <path d="M0 9 Q10 0 20 9" fill="none" stroke={colors.accent} strokeWidth="1.2" opacity="0.3" strokeLinecap="round" />
    </g>
  );

  const GlobeDoodle = ({ x, y }: { x: number; y: number }) => (
    <g transform={`translate(${x}, ${y})`}>
      <circle cx="12" cy="12" r="11" fill="none" stroke={colors.accent} strokeWidth="1.2" opacity="0.5" />
      <ellipse cx="12" cy="12" rx="11" ry="4" fill="none" stroke={colors.accent} strokeWidth="0.8" opacity="0.3" />
      <ellipse cx="12" cy="12" rx="4" ry="11" fill="none" stroke={colors.accent} strokeWidth="0.8" opacity="0.3" />
    </g>
  );

  const NodeDoodle = ({ x, y }: { x: number; y: number }) => (
    <g transform={`translate(${x}, ${y})`}>
      <circle cx="10" cy="10" r="8" fill="none" stroke={colors.accent} strokeWidth="1.2" opacity="0.5" />
      <circle cx="10" cy="10" r="3" fill={colors.accent} opacity="0.4" />
      <line x1="18" y1="10" x2="24" y2="10" stroke={colors.accent} strokeWidth="1" opacity="0.3" strokeDasharray="2 2" />
      <line x1="10" y1="18" x2="10" y2="24" stroke={colors.accent} strokeWidth="1" opacity="0.3" strokeDasharray="2 2" />
    </g>
  );

  // Create a repeating pattern of doodles
  const doodles = [
    { Component: ServerDoodle, x: 0, y: 48 },
    { Component: CloudDoodle, x: 35, y: 20 },
    { Component: DatabaseDoodle, x: 85, y: 50 },
    { Component: WifiDoodle, x: 120, y: 15 },
    { Component: NodeDoodle, x: 155, y: 55 },
    { Component: GlobeDoodle, x: 195, y: 25 },
    { Component: ServerDoodle, x: 235, y: 60 },
    { Component: CloudDoodle, x: 270, y: 35 },
    // Repeat for seamless scroll
    { Component: DatabaseDoodle, x: 320, y: 50 },
    { Component: WifiDoodle, x: 355, y: 15 },
    { Component: NodeDoodle, x: 390, y: 55 },
    { Component: GlobeDoodle, x: 430, y: 25 },
  ];

  return (
    <div className={cn("w-full h-full overflow-hidden", className)} style={{ backgroundColor: colors.bg }}>
      {/* Dotted background */}
      <svg className="absolute inset-0 w-full h-full" xmlns="http://www.w3.org/2000/svg">
        <pattern id={`dots-${toolsetSlug}`} x="0" y="0" width="16" height="16" patternUnits="userSpaceOnUse">
          <circle cx="8" cy="8" r="1" fill={colors.accent} opacity="0.15" />
        </pattern>
        <rect width="100%" height="100%" fill={`url(#dots-${toolsetSlug})`} />
      </svg>

      {/* Parallax scrolling doodles */}
      <svg
        viewBox="0 0 280 120"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        preserveAspectRatio="xMidYMid slice"
        className="w-full h-full relative"
        aria-hidden="true"
      >
        <g className="transition-transform duration-700 ease-out group-hover:-translate-x-12">
          {doodles.map((doodle, i) => (
            <doodle.Component key={i} x={doodle.x} y={doodle.y} />
          ))}
        </g>
      </svg>

      {/* MCP URL overlay */}
      <div
        className="absolute top-3 right-2"
        style={{
          backgroundColor: 'rgba(255, 255, 255, 0.75)',
          backdropFilter: 'blur(8px)',
          padding: '3px 6px',
          borderRadius: '4px',
          border: `1px solid ${colors.accent}40`,
        }}
      >
        <span
          className="text-[8px] font-mono font-medium whitespace-nowrap"
          style={{ color: colors.text }}
        >
          {displayUrl}
        </span>
      </div>
    </div>
  );
}

/**
 * Large hero illustration for MCP details page
 * Repeating hand-drawn doodles with slow continuous scroll animation
 */
export function MCPHeroIllustration({
  className,
  toolsetSlug,
  mcpUrl: _mcpUrl,
}: IllustrationProps & { toolsetSlug: string; mcpUrl?: string }) {
  const colors = hashStringToColor(toolsetSlug);

  // Larger doodle icons for hero
  const ServerDoodle = ({ x, y }: { x: number; y: number }) => (
    <g transform={`translate(${x}, ${y})`}>
      <rect x="0" y="0" width="45" height="60" rx="4" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.5" />
      <line x1="8" y1="15" x2="37" y2="15" stroke={colors.accent} strokeWidth="1.5" opacity="0.4" />
      <line x1="8" y1="27" x2="37" y2="27" stroke={colors.accent} strokeWidth="1.5" opacity="0.4" />
      <line x1="8" y1="39" x2="37" y2="39" stroke={colors.accent} strokeWidth="1.5" opacity="0.4" />
      <circle cx="12" cy="52" r="3" fill={colors.accent} opacity="0.5" />
      <circle cx="23" cy="52" r="3" fill={colors.accent} opacity="0.3" />
    </g>
  );

  const CloudDoodle = ({ x, y }: { x: number; y: number }) => (
    <g transform={`translate(${x}, ${y})`}>
      <path
        d="M12 45 Q0 45 0 30 Q0 15 15 15 Q15 0 35 0 Q55 0 60 15 Q75 10 80 25 Q90 25 90 35 Q90 45 80 45 Z"
        fill="none"
        stroke={colors.accent}
        strokeWidth="2"
        opacity="0.45"
        strokeLinecap="round"
      />
    </g>
  );

  const DatabaseDoodle = ({ x, y }: { x: number; y: number }) => (
    <g transform={`translate(${x}, ${y})`}>
      <ellipse cx="25" cy="10" rx="25" ry="10" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.5" />
      <path d="M0 10 L0 50 Q0 60 25 60 Q50 60 50 50 L50 10" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.5" />
      <ellipse cx="25" cy="50" rx="25" ry="10" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.3" />
      <ellipse cx="25" cy="30" rx="25" ry="10" fill="none" stroke={colors.accent} strokeWidth="1" opacity="0.2" strokeDasharray="4 3" />
    </g>
  );

  const WifiDoodle = ({ x, y }: { x: number; y: number }) => (
    <g transform={`translate(${x}, ${y})`}>
      <circle cx="25" cy="50" r="5" fill={colors.accent} opacity="0.5" />
      <path d="M10 35 Q25 20 40 35" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.4" strokeLinecap="round" />
      <path d="M0 22 Q25 0 50 22" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.3" strokeLinecap="round" />
    </g>
  );

  const GlobeDoodle = ({ x, y }: { x: number; y: number }) => (
    <g transform={`translate(${x}, ${y})`}>
      <circle cx="30" cy="30" r="28" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.45" />
      <ellipse cx="30" cy="30" rx="28" ry="10" fill="none" stroke={colors.accent} strokeWidth="1.2" opacity="0.3" />
      <ellipse cx="30" cy="30" rx="10" ry="28" fill="none" stroke={colors.accent} strokeWidth="1.2" opacity="0.3" />
    </g>
  );

  const NodeDoodle = ({ x, y }: { x: number; y: number }) => (
    <g transform={`translate(${x}, ${y})`}>
      <circle cx="25" cy="25" r="20" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.45" />
      <circle cx="25" cy="25" r="8" fill={colors.accent} opacity="0.35" />
      <line x1="45" y1="25" x2="60" y2="25" stroke={colors.accent} strokeWidth="1.5" opacity="0.3" strokeDasharray="4 3" />
      <line x1="25" y1="45" x2="25" y2="60" stroke={colors.accent} strokeWidth="1.5" opacity="0.3" strokeDasharray="4 3" />
    </g>
  );

  // Create rows of doodles that repeat for seamless scroll
  const heroDoodles = [
    // Row 1 (top)
    { Component: CloudDoodle, x: 0, y: 30 },
    { Component: ServerDoodle, x: 120, y: 50 },
    { Component: NodeDoodle, x: 200, y: 20 },
    { Component: DatabaseDoodle, x: 300, y: 55 },
    { Component: WifiDoodle, x: 400, y: 25 },
    { Component: GlobeDoodle, x: 480, y: 60 },
    { Component: ServerDoodle, x: 560, y: 30 },
    { Component: CloudDoodle, x: 650, y: 50 },
    // Row 2 (middle)
    { Component: DatabaseDoodle, x: 50, y: 130 },
    { Component: WifiDoodle, x: 150, y: 145 },
    { Component: GlobeDoodle, x: 240, y: 120 },
    { Component: CloudDoodle, x: 340, y: 150 },
    { Component: ServerDoodle, x: 450, y: 125 },
    { Component: NodeDoodle, x: 530, y: 150 },
    { Component: DatabaseDoodle, x: 620, y: 120 },
    // Row 3 (bottom)
    { Component: NodeDoodle, x: 20, y: 210 },
    { Component: GlobeDoodle, x: 100, y: 230 },
    { Component: ServerDoodle, x: 200, y: 205 },
    { Component: WifiDoodle, x: 280, y: 235 },
    { Component: CloudDoodle, x: 370, y: 210 },
    { Component: DatabaseDoodle, x: 480, y: 230 },
    { Component: NodeDoodle, x: 570, y: 205 },
    { Component: ServerDoodle, x: 660, y: 225 },
    // Repeat for seamless loop
    { Component: CloudDoodle, x: 750, y: 30 },
    { Component: ServerDoodle, x: 870, y: 50 },
    { Component: NodeDoodle, x: 950, y: 20 },
    { Component: WifiDoodle, x: 700, y: 145 },
    { Component: GlobeDoodle, x: 790, y: 120 },
    { Component: CloudDoodle, x: 890, y: 150 },
    { Component: GlobeDoodle, x: 750, y: 230 },
    { Component: ServerDoodle, x: 850, y: 205 },
  ];

  return (
    <div className={cn("w-full h-full relative overflow-hidden group", className)} style={{ backgroundColor: colors.bg }}>
      <style>{`
        @keyframes scrollLeft {
          0% { transform: translateX(0); }
          100% { transform: translateX(-200px); }
        }
      `}</style>

      {/* Dotted background */}
      <svg className="absolute inset-0 w-full h-full" xmlns="http://www.w3.org/2000/svg">
        <pattern id={`hero-dots-${toolsetSlug}`} x="0" y="0" width="24" height="24" patternUnits="userSpaceOnUse">
          <circle cx="12" cy="12" r="1.5" fill={colors.accent} opacity="0.12" />
        </pattern>
        <rect width="100%" height="100%" fill={`url(#hero-dots-${toolsetSlug})`} />
      </svg>

      {/* Scrolling doodles */}
      <svg
        viewBox="0 0 800 300"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        preserveAspectRatio="xMidYMid slice"
        className="w-full h-full relative"
        aria-hidden="true"
      >
        <g className="animate-[scrollLeft_30s_linear_infinite]">
          {heroDoodles.map((doodle, i) => (
            <doodle.Component key={i} x={doodle.x} y={doodle.y} />
          ))}
        </g>
      </svg>
    </div>
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
      <div className={cn("w-full h-full bg-violet-500/5 flex items-center justify-center", className)}>
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
      <rect x="30" y="35" width="50" height="50" rx="6" className="fill-violet-500/20 stroke-violet-500/30" strokeWidth="1.5" />
      <rect x="40" y="45" width="30" height="4" rx="1" className="fill-violet-500/40" />
      <rect x="40" y="53" width="20" height="4" rx="1" className="fill-violet-500/30" />
      <circle cx="45" cy="72" r="3" className="fill-emerald-500/50" />
      <circle cx="55" cy="72" r="3" className="fill-emerald-500/50" />
      <circle cx="65" cy="72" r="3" className="fill-violet-500/30" />

      {/* Connection lines */}
      <path d="M80 60 L115 60" className="stroke-violet-500/40" strokeWidth="2" strokeDasharray="4 2" />
      <circle cx="115" cy="60" r="4" className="fill-violet-500/30" />

      {/* Central hub */}
      <rect x="125" y="40" width="30" height="40" rx="4" className="fill-violet-500/25 stroke-violet-500/35" strokeWidth="1.5" />
      <rect x="132" y="48" width="16" height="3" rx="1" className="fill-violet-500/50" />
      <rect x="132" y="55" width="16" height="3" rx="1" className="fill-violet-500/40" />
      <rect x="132" y="62" width="16" height="3" rx="1" className="fill-violet-500/40" />
      <rect x="132" y="69" width="16" height="3" rx="1" className="fill-violet-500/30" />

      {/* Connection to right */}
      <circle cx="165" cy="60" r="4" className="fill-violet-500/30" />
      <path d="M165 60 L200 60" className="stroke-violet-500/40" strokeWidth="2" strokeDasharray="4 2" />

      {/* Server box right */}
      <rect x="200" y="35" width="50" height="50" rx="6" className="fill-violet-500/20 stroke-violet-500/30" strokeWidth="1.5" />
      <rect x="210" y="45" width="30" height="4" rx="1" className="fill-violet-500/40" />
      <rect x="210" y="53" width="25" height="4" rx="1" className="fill-violet-500/30" />
      <circle cx="215" cy="72" r="3" className="fill-emerald-500/50" />
      <circle cx="225" cy="72" r="3" className="fill-violet-500/30" />
      <circle cx="235" cy="72" r="3" className="fill-emerald-500/50" />
    </svg>
  );
}
