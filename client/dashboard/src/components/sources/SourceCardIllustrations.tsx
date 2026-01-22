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


// Generate deterministic pattern index from string
function getIllustrationPattern(str: string): number {
  let hash = 0;
  for (let i = 0; i < str.length; i++) {
    hash = str.charCodeAt(i) + ((hash << 5) - hash);
  }
  return Math.abs(hash) % 6;
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
 * MCP Card illustration with parallax hover effect
 * Uses pastel colors and whimsical server-themed patterns
 */
export function MCPIllustration({
  className,
  mcpUrl,
  toolsetSlug,
}: IllustrationProps & { mcpUrl: string; toolsetSlug: string }) {
  const colors = hashStringToColor(toolsetSlug);
  const pattern = getIllustrationPattern(toolsetSlug);
  const displayUrl = mcpUrl.replace(/^https?:\/\//, '');

  const renderPattern = () => {
    switch (pattern) {
      case 0: // Happy clouds
        return (
          <>
            {/* Cloud 1 - moves up-left on hover */}
            <g className="transition-transform duration-700 ease-out group-hover:-translate-x-1 group-hover:-translate-y-2">
              <circle cx="60" cy="35" r="12" fill={colors.accent} opacity="0.2" className="transition-opacity duration-700 group-hover:opacity-30" />
              <circle cx="72" cy="35" r="10" fill={colors.accent} opacity="0.2" className="transition-opacity duration-700 group-hover:opacity-30" />
              <circle cx="66" cy="28" r="8" fill={colors.accent} opacity="0.2" className="transition-opacity duration-700 group-hover:opacity-30" />
            </g>
            {/* Cloud 2 - moves right on hover */}
            <g className="transition-transform duration-700 ease-out group-hover:translate-x-2 group-hover:-translate-y-1">
              <circle cx="180" cy="55" r="14" fill={colors.accent} opacity="0.18" className="transition-opacity duration-700 group-hover:opacity-28" />
              <circle cx="195" cy="55" r="12" fill={colors.accent} opacity="0.18" className="transition-opacity duration-700 group-hover:opacity-28" />
              <circle cx="188" cy="47" r="10" fill={colors.accent} opacity="0.18" className="transition-opacity duration-700 group-hover:opacity-28" />
            </g>
            {/* Cloud 3 - moves down-left on hover */}
            <g className="transition-transform duration-700 ease-out group-hover:-translate-x-2 group-hover:translate-y-1">
              <circle cx="110" cy="80" r="10" fill={colors.accent} opacity="0.22" className="transition-opacity duration-700 group-hover:opacity-32" />
              <circle cx="120" cy="80" r="9" fill={colors.accent} opacity="0.22" className="transition-opacity duration-700 group-hover:opacity-32" />
              <circle cx="115" cy="74" r="7" fill={colors.accent} opacity="0.22" className="transition-opacity duration-700 group-hover:opacity-32" />
            </g>
            {/* Little sparkles - pulse on hover */}
            <circle cx="45" cy="28" r="2" fill={colors.accent} opacity="0.4" className="transition-all duration-500 group-hover:opacity-70 group-hover:scale-125" />
            <circle cx="220" cy="45" r="2" fill={colors.accent} opacity="0.4" className="transition-all duration-500 group-hover:opacity-70 group-hover:scale-125" />
          </>
        );

      case 1: // Flying data packets
        return (
          <>
            {/* Packet 1 - flies forward on hover */}
            <g className="transition-transform duration-700 ease-out group-hover:translate-x-4 group-hover:-translate-y-1">
              <rect x="45" y="30" width="20" height="14" rx="3" fill={colors.accent} opacity="0.25" className="transition-opacity duration-700 group-hover:opacity-35" />
              <path d="M55,30 L55,25 L65,30" stroke={colors.accent} strokeWidth="1.5" opacity="0.3" fill="none" className="transition-opacity duration-700 group-hover:opacity-50" />
            </g>
            {/* Packet 2 - flies forward slower */}
            <g className="transition-transform duration-700 ease-out group-hover:translate-x-3 group-hover:translate-y-1">
              <rect x="150" y="50" width="18" height="12" rx="3" fill={colors.accent} opacity="0.2" className="transition-opacity duration-700 group-hover:opacity-30" />
              <path d="M159,50 L159,45 L168,50" stroke={colors.accent} strokeWidth="1.5" opacity="0.25" fill="none" className="transition-opacity duration-700 group-hover:opacity-40" />
            </g>
            {/* Packet 3 - flies forward fastest */}
            <g className="transition-transform duration-700 ease-out group-hover:translate-x-5">
              <rect x="200" y="75" width="22" height="15" rx="3" fill={colors.accent} opacity="0.22" className="transition-opacity duration-700 group-hover:opacity-32" />
              <path d="M211,75 L211,70 L222,75" stroke={colors.accent} strokeWidth="1.5" opacity="0.28" fill="none" className="transition-opacity duration-700 group-hover:opacity-45" />
            </g>
            {/* Motion lines - extend on hover */}
            <line x1="70" y1="37" x2="85" y2="37" stroke={colors.accent} strokeWidth="1" opacity="0.2" strokeDasharray="2 2" className="transition-opacity duration-700 group-hover:opacity-40" />
            <line x1="175" y1="56" x2="190" y2="56" stroke={colors.accent} strokeWidth="1" opacity="0.15" strokeDasharray="2 2" className="transition-opacity duration-700 group-hover:opacity-35" />
          </>
        );

      case 2: // Cute server racks
        return (
          <>
            {/* Server 1 - shifts up on hover */}
            <g className="transition-transform duration-700 ease-out group-hover:-translate-y-2 group-hover:-translate-x-1">
              <rect x="50" y="35" width="30" height="40" rx="3" fill={colors.accent} opacity="0.15" className="transition-opacity duration-700 group-hover:opacity-25" />
              <rect x="54" y="40" width="22" height="6" rx="1" fill={colors.accent} opacity="0.3" className="transition-opacity duration-700 group-hover:opacity-40" />
              <rect x="54" y="50" width="22" height="6" rx="1" fill={colors.accent} opacity="0.3" className="transition-opacity duration-700 group-hover:opacity-40" />
              <rect x="54" y="60" width="22" height="6" rx="1" fill={colors.accent} opacity="0.3" className="transition-opacity duration-700 group-hover:opacity-40" />
              <circle cx="58" cy="43" r="1.5" fill={colors.accent} opacity="0.5" className="transition-opacity duration-500 group-hover:opacity-80" />
              <circle cx="58" cy="53" r="1.5" fill={colors.accent} opacity="0.5" className="transition-opacity duration-500 group-hover:opacity-80" />
            </g>
            {/* Server 2 - shifts down on hover */}
            <g className="transition-transform duration-700 ease-out group-hover:translate-y-1 group-hover:translate-x-1">
              <rect x="170" y="45" width="35" height="45" rx="3" fill={colors.accent} opacity="0.12" className="transition-opacity duration-700 group-hover:opacity-22" />
              <rect x="175" y="51" width="25" height="7" rx="1" fill={colors.accent} opacity="0.28" className="transition-opacity duration-700 group-hover:opacity-38" />
              <rect x="175" y="62" width="25" height="7" rx="1" fill={colors.accent} opacity="0.28" className="transition-opacity duration-700 group-hover:opacity-38" />
              <rect x="175" y="73" width="25" height="7" rx="1" fill={colors.accent} opacity="0.28" className="transition-opacity duration-700 group-hover:opacity-38" />
              <circle cx="180" cy="54" r="1.5" fill={colors.accent} opacity="0.5" className="transition-opacity duration-500 group-hover:opacity-80" />
            </g>
          </>
        );

      case 3: // Database cylinders
        return (
          <>
            {/* DB 1 - lifts up on hover */}
            <g className="transition-transform duration-700 ease-out group-hover:-translate-y-2 group-hover:-translate-x-1">
              <ellipse cx="70" cy="35" rx="18" ry="6" fill={colors.accent} opacity="0.25" className="transition-opacity duration-700 group-hover:opacity-35" />
              <rect x="52" y="35" width="36" height="25" fill={colors.accent} opacity="0.2" className="transition-opacity duration-700 group-hover:opacity-30" />
              <ellipse cx="70" cy="60" rx="18" ry="6" fill={colors.accent} opacity="0.25" className="transition-opacity duration-700 group-hover:opacity-35" />
              <ellipse cx="70" cy="45" rx="18" ry="6" fill="none" stroke={colors.accent} strokeWidth="1" opacity="0.3" />
            </g>
            {/* DB 2 - lifts up slower on hover */}
            <g className="transition-transform duration-700 ease-out group-hover:-translate-y-3">
              <ellipse cx="180" cy="45" rx="22" ry="7" fill={colors.accent} opacity="0.2" className="transition-opacity duration-700 group-hover:opacity-30" />
              <rect x="158" y="45" width="44" height="30" fill={colors.accent} opacity="0.15" className="transition-opacity duration-700 group-hover:opacity-25" />
              <ellipse cx="180" cy="75" rx="22" ry="7" fill={colors.accent} opacity="0.2" className="transition-opacity duration-700 group-hover:opacity-30" />
              <ellipse cx="180" cy="55" rx="22" ry="7" fill="none" stroke={colors.accent} strokeWidth="1" opacity="0.25" />
              <ellipse cx="180" cy="65" rx="22" ry="7" fill="none" stroke={colors.accent} strokeWidth="1" opacity="0.2" />
            </g>
            {/* Sync lines */}
            <line x1="92" y1="50" x2="155" y2="55" stroke={colors.accent} strokeWidth="1" opacity="0.2" strokeDasharray="3 3" className="transition-opacity duration-500 group-hover:opacity-40" />
          </>
        );

      case 4: // Signal broadcast
        return (
          <>
            {/* Antenna - pulses on hover */}
            <g className="transition-transform duration-500 group-hover:-translate-y-1">
              <line x1="140" y1="45" x2="140" y2="85" stroke={colors.accent} strokeWidth="2" opacity="0.4" />
              <circle cx="140" cy="40" r="4" fill={colors.accent} opacity="0.5" className="transition-all duration-500 group-hover:opacity-80" />
            </g>
            {/* Signal waves - expand on hover */}
            <g className="transition-all duration-700 group-hover:scale-110" style={{ transformOrigin: '140px 40px' }}>
              <path d="M120 40 Q130 25 140 40 Q150 55 160 40" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.2" className="transition-opacity duration-500 group-hover:opacity-35" />
              <path d="M105 40 Q122 15 140 40 Q158 65 175 40" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.15" className="transition-opacity duration-700 group-hover:opacity-25" />
              <path d="M90 40 Q115 5 140 40 Q165 75 190 40" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.1" className="transition-opacity duration-900 group-hover:opacity-18" />
            </g>
            {/* Small receiver dots */}
            <circle cx="60" cy="70" r="5" fill={colors.accent} opacity="0.25" className="transition-all duration-500 group-hover:opacity-45" />
            <circle cx="220" cy="65" r="5" fill={colors.accent} opacity="0.25" className="transition-all duration-700 group-hover:opacity-45" />
          </>
        );

      case 5: // Network nodes
      default:
        return (
          <>
            {/* Central node */}
            <g className="transition-transform duration-500 group-hover:scale-110" style={{ transformOrigin: '140px 60px' }}>
              <circle cx="140" cy="60" r="16" fill={colors.accent} opacity="0.3" className="transition-opacity duration-500 group-hover:opacity-45" />
              <circle cx="140" cy="60" r="8" fill={colors.accent} opacity="0.5" className="transition-opacity duration-500 group-hover:opacity-70" />
            </g>
            {/* Surrounding nodes - spread out on hover */}
            <g className="transition-transform duration-700 group-hover:-translate-x-2 group-hover:-translate-y-2">
              <circle cx="70" cy="35" r="8" fill={colors.accent} opacity="0.25" className="transition-opacity duration-500 group-hover:opacity-40" />
            </g>
            <g className="transition-transform duration-700 group-hover:translate-x-2 group-hover:-translate-y-2">
              <circle cx="210" cy="35" r="8" fill={colors.accent} opacity="0.25" className="transition-opacity duration-500 group-hover:opacity-40" />
            </g>
            <g className="transition-transform duration-700 group-hover:-translate-x-2 group-hover:translate-y-2">
              <circle cx="70" cy="85" r="8" fill={colors.accent} opacity="0.25" className="transition-opacity duration-500 group-hover:opacity-40" />
            </g>
            <g className="transition-transform duration-700 group-hover:translate-x-2 group-hover:translate-y-2">
              <circle cx="210" cy="85" r="8" fill={colors.accent} opacity="0.25" className="transition-opacity duration-500 group-hover:opacity-40" />
            </g>
            {/* Connection lines */}
            <line x1="140" y1="60" x2="70" y2="35" stroke={colors.accent} strokeWidth="1.5" opacity="0.2" strokeDasharray="3 3" className="transition-opacity duration-500 group-hover:opacity-35" />
            <line x1="140" y1="60" x2="210" y2="35" stroke={colors.accent} strokeWidth="1.5" opacity="0.2" strokeDasharray="3 3" className="transition-opacity duration-500 group-hover:opacity-35" />
            <line x1="140" y1="60" x2="70" y2="85" stroke={colors.accent} strokeWidth="1.5" opacity="0.2" strokeDasharray="3 3" className="transition-opacity duration-500 group-hover:opacity-35" />
            <line x1="140" y1="60" x2="210" y2="85" stroke={colors.accent} strokeWidth="1.5" opacity="0.2" strokeDasharray="3 3" className="transition-opacity duration-500 group-hover:opacity-35" />
          </>
        );
    }
  };

  return (
    <svg
      viewBox="0 0 280 120"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      preserveAspectRatio="xMidYMid slice"
      className={cn("w-full h-full", className)}
      aria-hidden="true"
    >
      {/* Background with pastel color */}
      <rect width="280" height="120" fill={colors.bg} />

      {/* Decorative grid pattern */}
      <pattern id={`grid-${toolsetSlug}`} x="0" y="0" width="20" height="20" patternUnits="userSpaceOnUse">
        <circle cx="2" cy="2" r="0.5" fill={colors.accent} opacity="0.15" />
      </pattern>
      <rect width="280" height="120" fill={`url(#grid-${toolsetSlug})`} />

      {/* Unique whimsical pattern */}
      {renderPattern()}

      {/* MCP URL overlay */}
      <foreignObject x="0" y="0" width="280" height="120">
        <div
          xmlns="http://www.w3.org/1999/xhtml"
          style={{
            width: '100%',
            height: '100%',
            position: 'relative',
          }}
        >
          <div
            style={{
              position: 'absolute',
              top: '12px',
              right: '8px',
              backgroundColor: 'rgba(255, 255, 255, 0.7)',
              backdropFilter: 'blur(8px)',
              padding: '4px 8px',
              borderRadius: '4px',
              border: `1px solid ${colors.accent}40`,
            }}
          >
            <div
              style={{
                fontSize: '8px',
                fontFamily: 'ui-monospace, monospace',
                color: colors.text,
                whiteSpace: 'nowrap',
                fontWeight: 500,
              }}
            >
              {displayUrl}
            </div>
          </div>
        </div>
      </foreignObject>
    </svg>
  );
}

/**
 * Large hero illustration for MCP details page
 */
export function MCPHeroIllustration({
  className,
  toolsetSlug,
  mcpUrl: _mcpUrl,
}: IllustrationProps & { toolsetSlug: string; mcpUrl?: string }) {
  const colors = hashStringToColor(toolsetSlug);
  const pattern = getIllustrationPattern(toolsetSlug);

  const renderHeroPattern = () => {
    switch (pattern) {
      case 0: // Floating clouds
        return (
          <>
            {/* Cloud clusters */}
            <g className="animate-[float_6s_ease-in-out_infinite]">
              <circle cx="120" cy="100" r="35" fill={colors.accent} opacity="0.2" />
              <circle cx="150" cy="100" r="30" fill={colors.accent} opacity="0.2" />
              <circle cx="135" cy="80" r="25" fill={colors.accent} opacity="0.2" />
            </g>
            <g className="animate-[float_8s_ease-in-out_infinite]" style={{ animationDelay: '2s' }}>
              <circle cx="550" cy="150" r="45" fill={colors.accent} opacity="0.15" />
              <circle cx="590" cy="150" r="38" fill={colors.accent} opacity="0.15" />
              <circle cx="570" cy="120" r="32" fill={colors.accent} opacity="0.15" />
            </g>
            <g className="animate-[float_7s_ease-in-out_infinite]" style={{ animationDelay: '4s' }}>
              <circle cx="350" cy="220" r="28" fill={colors.accent} opacity="0.18" />
              <circle cx="375" cy="220" r="24" fill={colors.accent} opacity="0.18" />
              <circle cx="362" cy="200" r="20" fill={colors.accent} opacity="0.18" />
            </g>
            {/* Sparkles */}
            <circle cx="200" cy="60" r="5" fill={colors.accent} opacity="0.4" className="animate-pulse" />
            <circle cx="650" cy="80" r="4" fill={colors.accent} opacity="0.4" className="animate-pulse" style={{ animationDelay: '1s' }} />
            <circle cx="450" cy="250" r="6" fill={colors.accent} opacity="0.35" className="animate-pulse" style={{ animationDelay: '2s' }} />
          </>
        );

      case 1: // Data packets flowing
        return (
          <>
            {/* Packets */}
            <g className="animate-[flow_4s_linear_infinite]">
              <rect x="80" y="100" width="40" height="28" rx="6" fill={colors.accent} opacity="0.25" />
              <path d="M100,100 L100,90 L120,100" stroke={colors.accent} strokeWidth="2" fill="none" opacity="0.35" />
            </g>
            <g className="animate-[flow_5s_linear_infinite]" style={{ animationDelay: '1.5s' }}>
              <rect x="300" y="160" width="35" height="24" rx="5" fill={colors.accent} opacity="0.2" />
              <path d="M317,160 L317,150 L335,160" stroke={colors.accent} strokeWidth="2" fill="none" opacity="0.3" />
            </g>
            <g className="animate-[flow_4.5s_linear_infinite]" style={{ animationDelay: '3s' }}>
              <rect x="550" y="120" width="45" height="32" rx="7" fill={colors.accent} opacity="0.22" />
              <path d="M572,120 L572,108 L595,120" stroke={colors.accent} strokeWidth="2" fill="none" opacity="0.32" />
            </g>
            {/* Flow lines */}
            <line x1="130" y1="114" x2="280" y2="168" stroke={colors.accent} strokeWidth="2" opacity="0.15" strokeDasharray="8 8" />
            <line x1="345" y1="172" x2="540" y2="136" stroke={colors.accent} strokeWidth="2" opacity="0.15" strokeDasharray="8 8" />
          </>
        );

      case 2: // Server racks
        return (
          <>
            {/* Server 1 */}
            <g className="animate-[lift_3s_ease-in-out_infinite]">
              <rect x="100" y="80" width="80" height="120" rx="8" fill={colors.accent} opacity="0.15" />
              <rect x="110" y="95" width="60" height="18" rx="3" fill={colors.accent} opacity="0.3" />
              <rect x="110" y="120" width="60" height="18" rx="3" fill={colors.accent} opacity="0.3" />
              <rect x="110" y="145" width="60" height="18" rx="3" fill={colors.accent} opacity="0.3" />
              <circle cx="120" cy="104" r="4" fill={colors.accent} opacity="0.6" className="animate-pulse" />
              <circle cx="120" cy="129" r="4" fill={colors.accent} opacity="0.6" className="animate-pulse" style={{ animationDelay: '0.5s' }} />
              <circle cx="120" cy="154" r="4" fill={colors.accent} opacity="0.6" className="animate-pulse" style={{ animationDelay: '1s' }} />
            </g>
            {/* Server 2 */}
            <g className="animate-[lift_4s_ease-in-out_infinite]" style={{ animationDelay: '1s' }}>
              <rect x="550" y="100" width="90" height="130" rx="8" fill={colors.accent} opacity="0.12" />
              <rect x="562" y="118" width="66" height="20" rx="3" fill={colors.accent} opacity="0.28" />
              <rect x="562" y="145" width="66" height="20" rx="3" fill={colors.accent} opacity="0.28" />
              <rect x="562" y="172" width="66" height="20" rx="3" fill={colors.accent} opacity="0.28" />
              <circle cx="574" cy="128" r="4" fill={colors.accent} opacity="0.55" className="animate-pulse" style={{ animationDelay: '0.3s' }} />
              <circle cx="574" cy="155" r="4" fill={colors.accent} opacity="0.55" className="animate-pulse" style={{ animationDelay: '0.8s' }} />
            </g>
            {/* Connection */}
            <line x1="190" y1="140" x2="540" y2="165" stroke={colors.accent} strokeWidth="2" opacity="0.2" strokeDasharray="10 6" />
          </>
        );

      case 3: // Database cylinders
        return (
          <>
            {/* DB 1 */}
            <g className="animate-[lift_4s_ease-in-out_infinite]">
              <ellipse cx="180" cy="100" rx="50" ry="18" fill={colors.accent} opacity="0.25" />
              <rect x="130" y="100" width="100" height="80" fill={colors.accent} opacity="0.18" />
              <ellipse cx="180" cy="180" rx="50" ry="18" fill={colors.accent} opacity="0.25" />
              <ellipse cx="180" cy="130" rx="50" ry="18" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.25" />
              <ellipse cx="180" cy="155" rx="50" ry="18" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.2" />
            </g>
            {/* DB 2 */}
            <g className="animate-[lift_5s_ease-in-out_infinite]" style={{ animationDelay: '1.5s' }}>
              <ellipse cx="550" cy="120" rx="60" ry="20" fill={colors.accent} opacity="0.2" />
              <rect x="490" y="120" width="120" height="90" fill={colors.accent} opacity="0.14" />
              <ellipse cx="550" cy="210" rx="60" ry="20" fill={colors.accent} opacity="0.2" />
              <ellipse cx="550" cy="150" rx="60" ry="20" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.22" />
              <ellipse cx="550" cy="180" rx="60" ry="20" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.18" />
            </g>
            {/* Sync arrow */}
            <path d="M240 150 Q380 100 480 160" stroke={colors.accent} strokeWidth="2" opacity="0.2" fill="none" strokeDasharray="8 6" />
          </>
        );

      case 4: // Signal broadcast
        return (
          <>
            {/* Antenna */}
            <g className="animate-[pulse_2s_ease-in-out_infinite]">
              <line x1="400" y1="100" x2="400" y2="220" stroke={colors.accent} strokeWidth="4" opacity="0.4" />
              <circle cx="400" cy="90" r="12" fill={colors.accent} opacity="0.6" />
            </g>
            {/* Signal waves */}
            <g className="animate-[rings_3s_ease-out_infinite]">
              <path d="M340 90 Q370 40 400 90 Q430 140 460 90" fill="none" stroke={colors.accent} strokeWidth="2.5" opacity="0.25" />
              <path d="M300 90 Q350 10 400 90 Q450 170 500 90" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.18" />
              <path d="M260 90 Q330 -20 400 90 Q470 200 540 90" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.12" />
            </g>
            {/* Receiver nodes */}
            <circle cx="150" cy="200" r="15" fill={colors.accent} opacity="0.25" className="animate-pulse" style={{ animationDelay: '0.5s' }} />
            <circle cx="650" cy="180" r="18" fill={colors.accent} opacity="0.22" className="animate-pulse" style={{ animationDelay: '1s' }} />
            <circle cx="300" cy="250" r="12" fill={colors.accent} opacity="0.28" className="animate-pulse" style={{ animationDelay: '1.5s' }} />
          </>
        );

      case 5: // Network nodes
      default:
        return (
          <>
            {/* Central hub */}
            <g className="animate-[pulse_3s_ease-in-out_infinite]">
              <circle cx="400" cy="150" r="45" fill={colors.accent} opacity="0.25" />
              <circle cx="400" cy="150" r="25" fill={colors.accent} opacity="0.4" />
            </g>
            {/* Surrounding nodes */}
            <circle cx="180" cy="100" r="20" fill={colors.accent} opacity="0.2" className="animate-[float_4s_ease-in-out_infinite]" />
            <circle cx="620" cy="100" r="20" fill={colors.accent} opacity="0.2" className="animate-[float_5s_ease-in-out_infinite]" style={{ animationDelay: '1s' }} />
            <circle cx="180" cy="220" r="20" fill={colors.accent} opacity="0.2" className="animate-[float_4.5s_ease-in-out_infinite]" style={{ animationDelay: '2s' }} />
            <circle cx="620" cy="220" r="20" fill={colors.accent} opacity="0.2" className="animate-[float_5.5s_ease-in-out_infinite]" style={{ animationDelay: '0.5s' }} />
            {/* Connection lines */}
            <line x1="400" y1="150" x2="180" y2="100" stroke={colors.accent} strokeWidth="2" opacity="0.2" strokeDasharray="6 4" />
            <line x1="400" y1="150" x2="620" y2="100" stroke={colors.accent} strokeWidth="2" opacity="0.2" strokeDasharray="6 4" />
            <line x1="400" y1="150" x2="180" y2="220" stroke={colors.accent} strokeWidth="2" opacity="0.2" strokeDasharray="6 4" />
            <line x1="400" y1="150" x2="620" y2="220" stroke={colors.accent} strokeWidth="2" opacity="0.2" strokeDasharray="6 4" />
          </>
        );
    }
  };

  return (
    <div className={cn("w-full h-full relative overflow-hidden", className)} style={{ backgroundColor: colors.bg }}>
      <style>{`
        @keyframes float {
          0%, 100% { transform: translateY(0); }
          50% { transform: translateY(-10px); }
        }
        @keyframes flow {
          0% { transform: translateX(0); }
          100% { transform: translateX(100px); opacity: 0; }
        }
        @keyframes lift {
          0%, 100% { transform: translateY(0); }
          50% { transform: translateY(-8px); }
        }
        @keyframes rings {
          0% { transform: scale(1); opacity: 0.3; }
          100% { transform: scale(1.3); opacity: 0; }
        }
      `}</style>

      {/* Grid pattern background */}
      <svg className="absolute inset-0 w-full h-full" xmlns="http://www.w3.org/2000/svg">
        <pattern id={`hero-grid-${toolsetSlug}`} x="0" y="0" width="30" height="30" patternUnits="userSpaceOnUse">
          <circle cx="3" cy="3" r="1" fill={colors.accent} opacity="0.12" />
        </pattern>
        <rect width="100%" height="100%" fill={`url(#hero-grid-${toolsetSlug})`} />
      </svg>

      {/* Animated elements */}
      <svg
        viewBox="0 0 800 300"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        preserveAspectRatio="xMidYMid slice"
        className="w-full h-full relative"
        aria-hidden="true"
      >
        {renderHeroPattern()}
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
