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
 * MCP Card illustration with hand-drawn doodle style
 * Sketch-like line work with organic, informal aesthetic
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
      case 0: // Doodle cloud with connection
        return (
          <g className="transition-transform duration-500 group-hover:-translate-y-1">
            {/* Cloud shape - hand drawn style */}
            <path
              d="M55 55 Q45 55 45 45 Q45 35 55 35 Q55 25 70 25 Q85 25 90 35 Q100 30 110 40 Q120 35 125 45 Q135 45 135 55 Q135 65 125 65 L55 65 Q45 65 45 55"
              fill="none"
              stroke={colors.accent}
              strokeWidth="1.5"
              opacity="0.6"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
            {/* Connection line to server */}
            <path d="M135 55 Q155 55 175 50" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.4" strokeDasharray="4 3" />
            {/* Small server box */}
            <rect x="175" y="35" width="35" height="45" rx="3" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.5" />
            <line x1="180" y1="48" x2="205" y2="48" stroke={colors.accent} strokeWidth="1.5" opacity="0.4" />
            <line x1="180" y1="58" x2="205" y2="58" stroke={colors.accent} strokeWidth="1.5" opacity="0.4" />
            <line x1="180" y1="68" x2="205" y2="68" stroke={colors.accent} strokeWidth="1.5" opacity="0.4" />
            {/* Status dots */}
            <circle cx="183" cy="43" r="2" fill={colors.accent} opacity="0.5" />
            <circle cx="190" cy="43" r="2" fill={colors.accent} opacity="0.3" />
          </g>
        );

      case 1: // Network nodes doodle
        return (
          <g className="transition-transform duration-500 group-hover:scale-[1.02]" style={{ transformOrigin: '140px 60px' }}>
            {/* Central hub - sketchy circle */}
            <circle cx="140" cy="60" r="18" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.5" />
            <circle cx="140" cy="60" r="6" fill={colors.accent} opacity="0.4" />
            {/* Outer nodes */}
            <circle cx="70" cy="40" r="10" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.45" />
            <circle cx="70" cy="40" r="3" fill={colors.accent} opacity="0.35" />
            <circle cx="210" cy="40" r="10" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.45" />
            <circle cx="210" cy="40" r="3" fill={colors.accent} opacity="0.35" />
            <circle cx="90" cy="90" r="8" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.4" />
            <circle cx="190" cy="90" r="8" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.4" />
            {/* Connection lines - dashed hand-drawn style */}
            <path d="M125 50 Q100 45 80 42" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.35" strokeDasharray="5 4" />
            <path d="M155 50 Q180 45 200 42" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.35" strokeDasharray="5 4" />
            <path d="M130 75 Q110 82 95 88" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.3" strokeDasharray="5 4" />
            <path d="M150 75 Q170 82 185 88" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.3" strokeDasharray="5 4" />
          </g>
        );

      case 2: // Server rack doodle
        return (
          <g className="transition-transform duration-500 group-hover:-translate-y-1">
            {/* Server 1 */}
            <rect x="45" y="30" width="50" height="65" rx="3" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.5" />
            <line x1="52" y1="45" x2="88" y2="45" stroke={colors.accent} strokeWidth="1.5" opacity="0.4" />
            <line x1="52" y1="58" x2="88" y2="58" stroke={colors.accent} strokeWidth="1.5" opacity="0.4" />
            <line x1="52" y1="71" x2="88" y2="71" stroke={colors.accent} strokeWidth="1.5" opacity="0.4" />
            <line x1="52" y1="84" x2="88" y2="84" stroke={colors.accent} strokeWidth="1.5" opacity="0.4" />
            {/* LED indicators */}
            <circle cx="55" cy="38" r="2" fill={colors.accent} opacity="0.5" />
            <circle cx="62" cy="38" r="2" fill={colors.accent} opacity="0.3" />
            {/* Server 2 */}
            <rect x="170" y="35" width="55" height="55" rx="3" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.45" />
            <line x1="178" y1="50" x2="218" y2="50" stroke={colors.accent} strokeWidth="1.5" opacity="0.35" />
            <line x1="178" y1="63" x2="218" y2="63" stroke={colors.accent} strokeWidth="1.5" opacity="0.35" />
            <line x1="178" y1="76" x2="218" y2="76" stroke={colors.accent} strokeWidth="1.5" opacity="0.35" />
            <circle cx="180" cy="43" r="2" fill={colors.accent} opacity="0.45" />
            {/* Connection between servers */}
            <path d="M95 62 Q130 55 170 62" fill="none" stroke={colors.accent} strokeWidth="1" opacity="0.3" strokeDasharray="3 3" />
          </g>
        );

      case 3: // Database doodle
        return (
          <g className="transition-transform duration-500 group-hover:-translate-y-1">
            {/* Database cylinder 1 */}
            <ellipse cx="75" cy="35" rx="25" ry="8" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.5" />
            <path d="M50 35 L50 75 Q50 83 75 83 Q100 83 100 75 L100 35" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.5" />
            <ellipse cx="75" cy="75" rx="25" ry="8" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.35" />
            <ellipse cx="75" cy="55" rx="25" ry="8" fill="none" stroke={colors.accent} strokeWidth="1" opacity="0.25" strokeDasharray="3 2" />
            {/* Database cylinder 2 */}
            <ellipse cx="190" cy="40" rx="30" ry="10" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.45" />
            <path d="M160 40 L160 85 Q160 95 190 95 Q220 95 220 85 L220 40" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.45" />
            <ellipse cx="190" cy="85" rx="30" ry="10" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.3" />
            <ellipse cx="190" cy="62" rx="30" ry="10" fill="none" stroke={colors.accent} strokeWidth="1" opacity="0.2" strokeDasharray="3 2" />
            {/* Sync arrow */}
            <path d="M105 55 L155 60" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.3" strokeDasharray="4 3" />
            <path d="M150 56 L155 60 L150 64" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.3" />
          </g>
        );

      case 4: // Wifi/Signal doodle
        return (
          <g className="transition-transform duration-500 group-hover:-translate-y-1">
            {/* Router box */}
            <rect x="110" y="65" width="60" height="30" rx="4" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.5" />
            <circle cx="125" cy="80" r="3" fill={colors.accent} opacity="0.4" />
            <circle cx="140" cy="80" r="3" fill={colors.accent} opacity="0.3" />
            <circle cx="155" cy="80" r="3" fill={colors.accent} opacity="0.4" />
            {/* Antenna */}
            <line x1="140" y1="65" x2="140" y2="45" stroke={colors.accent} strokeWidth="1.5" opacity="0.5" />
            <circle cx="140" cy="42" r="4" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.5" />
            {/* Signal waves */}
            <path d="M120 35 Q130 20 140 35" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.35" strokeLinecap="round" />
            <path d="M140 35 Q150 20 160 35" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.35" strokeLinecap="round" />
            <path d="M105 25 Q122 5 140 25" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.25" strokeLinecap="round" />
            <path d="M140 25 Q158 5 175 25" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.25" strokeLinecap="round" />
            {/* Connected devices */}
            <rect x="50" y="70" width="20" height="14" rx="2" fill="none" stroke={colors.accent} strokeWidth="1" opacity="0.35" />
            <rect x="210" y="70" width="20" height="14" rx="2" fill="none" stroke={colors.accent} strokeWidth="1" opacity="0.35" />
            <path d="M70 77 Q90 77 110 80" fill="none" stroke={colors.accent} strokeWidth="1" opacity="0.25" strokeDasharray="3 2" />
            <path d="M170 80 Q190 77 210 77" fill="none" stroke={colors.accent} strokeWidth="1" opacity="0.25" strokeDasharray="3 2" />
          </g>
        );

      case 5: // Globe/World doodle
      default:
        return (
          <g className="transition-transform duration-500 group-hover:rotate-[5deg]" style={{ transformOrigin: '140px 60px' }}>
            {/* Globe */}
            <circle cx="140" cy="60" r="35" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.5" />
            {/* Latitude lines */}
            <ellipse cx="140" cy="60" rx="35" ry="12" fill="none" stroke={colors.accent} strokeWidth="1" opacity="0.3" />
            <ellipse cx="140" cy="60" rx="28" ry="35" fill="none" stroke={colors.accent} strokeWidth="1" opacity="0.3" />
            {/* Meridian */}
            <line x1="140" y1="25" x2="140" y2="95" stroke={colors.accent} strokeWidth="1" opacity="0.25" />
            {/* Orbit ring */}
            <ellipse cx="140" cy="60" rx="50" ry="18" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.3" strokeDasharray="6 4" />
            {/* Satellite */}
            <g>
              <rect x="55" y="45" width="12" height="8" rx="1" fill="none" stroke={colors.accent} strokeWidth="1" opacity="0.4" />
              <line x1="50" y1="49" x2="55" y2="49" stroke={colors.accent} strokeWidth="1" opacity="0.4" />
              <line x1="67" y1="49" x2="72" y2="49" stroke={colors.accent} strokeWidth="1" opacity="0.4" />
            </g>
            {/* Connection points */}
            <circle cx="115" cy="50" r="3" fill={colors.accent} opacity="0.35" />
            <circle cx="165" cy="70" r="3" fill={colors.accent} opacity="0.35" />
            <circle cx="140" cy="90" r="3" fill={colors.accent} opacity="0.35" />
          </g>
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

      {/* Dotted background pattern */}
      <pattern id={`grid-${toolsetSlug}`} x="0" y="0" width="16" height="16" patternUnits="userSpaceOnUse">
        <circle cx="8" cy="8" r="1" fill={colors.accent} opacity="0.2" />
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
 * Hand-drawn doodle style matching card illustrations
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
      case 0: // Cloud network doodle
        return (
          <>
            {/* Large cloud */}
            <g className="animate-[float_6s_ease-in-out_infinite]">
              <path
                d="M100 150 Q70 150 70 120 Q70 90 100 90 Q100 60 140 60 Q180 60 190 90 Q220 80 240 100 Q270 90 280 110 Q310 110 310 140 Q310 170 280 170 L100 170 Q70 170 70 150"
                fill="none"
                stroke={colors.accent}
                strokeWidth="2"
                opacity="0.5"
                strokeLinecap="round"
              />
            </g>
            {/* Small cloud */}
            <g className="animate-[float_8s_ease-in-out_infinite]" style={{ animationDelay: '2s' }}>
              <path
                d="M500 180 Q480 180 480 160 Q480 140 500 140 Q500 120 530 120 Q560 120 565 140 Q580 135 590 150 Q600 150 600 165 Q600 180 580 180 Z"
                fill="none"
                stroke={colors.accent}
                strokeWidth="2"
                opacity="0.4"
                strokeLinecap="round"
              />
            </g>
            {/* Connection to servers */}
            <path d="M310 155 Q400 140 480 160" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.3" strokeDasharray="8 6" />
            {/* Server boxes */}
            <g className="animate-[float_7s_ease-in-out_infinite]" style={{ animationDelay: '1s' }}>
              <rect x="620" y="100" width="80" height="100" rx="4" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.45" />
              <line x1="635" y1="125" x2="685" y2="125" stroke={colors.accent} strokeWidth="2" opacity="0.35" />
              <line x1="635" y1="150" x2="685" y2="150" stroke={colors.accent} strokeWidth="2" opacity="0.35" />
              <line x1="635" y1="175" x2="685" y2="175" stroke={colors.accent} strokeWidth="2" opacity="0.35" />
              <circle cx="643" cy="113" r="4" fill={colors.accent} opacity="0.4" />
              <circle cx="656" cy="113" r="4" fill={colors.accent} opacity="0.25" />
            </g>
            <path d="M600 160 Q610 155 620 150" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.3" strokeDasharray="5 4" />
          </>
        );

      case 1: // Network topology doodle
        return (
          <>
            {/* Central hub */}
            <g className="animate-[pulse_3s_ease-in-out_infinite]">
              <circle cx="400" cy="150" r="40" fill="none" stroke={colors.accent} strokeWidth="2.5" opacity="0.5" />
              <circle cx="400" cy="150" r="15" fill={colors.accent} opacity="0.35" />
            </g>
            {/* Outer nodes */}
            <g className="animate-[float_4s_ease-in-out_infinite]">
              <circle cx="200" cy="100" r="25" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.4" />
              <circle cx="200" cy="100" r="8" fill={colors.accent} opacity="0.3" />
            </g>
            <g className="animate-[float_5s_ease-in-out_infinite]" style={{ animationDelay: '1s' }}>
              <circle cx="600" cy="100" r="25" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.4" />
              <circle cx="600" cy="100" r="8" fill={colors.accent} opacity="0.3" />
            </g>
            <g className="animate-[float_4.5s_ease-in-out_infinite]" style={{ animationDelay: '2s' }}>
              <circle cx="250" cy="230" r="20" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.35" />
              <circle cx="250" cy="230" r="6" fill={colors.accent} opacity="0.25" />
            </g>
            <g className="animate-[float_5.5s_ease-in-out_infinite]" style={{ animationDelay: '0.5s' }}>
              <circle cx="550" cy="230" r="20" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.35" />
              <circle cx="550" cy="230" r="6" fill={colors.accent} opacity="0.25" />
            </g>
            {/* Connection lines */}
            <path d="M365 130 Q280 110 225 105" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.3" strokeDasharray="8 5" />
            <path d="M435 130 Q520 110 575 105" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.3" strokeDasharray="8 5" />
            <path d="M375 180 Q310 205 268 222" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.25" strokeDasharray="8 5" />
            <path d="M425 180 Q490 205 532 222" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.25" strokeDasharray="8 5" />
          </>
        );

      case 2: // Server rack doodle
        return (
          <>
            {/* Server 1 */}
            <g className="animate-[lift_3s_ease-in-out_infinite]">
              <rect x="120" y="70" width="120" height="160" rx="6" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.5" />
              <line x1="135" y1="100" x2="225" y2="100" stroke={colors.accent} strokeWidth="2" opacity="0.4" />
              <line x1="135" y1="130" x2="225" y2="130" stroke={colors.accent} strokeWidth="2" opacity="0.4" />
              <line x1="135" y1="160" x2="225" y2="160" stroke={colors.accent} strokeWidth="2" opacity="0.4" />
              <line x1="135" y1="190" x2="225" y2="190" stroke={colors.accent} strokeWidth="2" opacity="0.4" />
              <circle cx="145" cy="85" r="5" fill={colors.accent} opacity="0.45" />
              <circle cx="162" cy="85" r="5" fill={colors.accent} opacity="0.3" />
            </g>
            {/* Server 2 */}
            <g className="animate-[lift_4s_ease-in-out_infinite]" style={{ animationDelay: '1s' }}>
              <rect x="560" y="80" width="130" height="150" rx="6" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.45" />
              <line x1="578" y1="110" x2="672" y2="110" stroke={colors.accent} strokeWidth="2" opacity="0.35" />
              <line x1="578" y1="140" x2="672" y2="140" stroke={colors.accent} strokeWidth="2" opacity="0.35" />
              <line x1="578" y1="170" x2="672" y2="170" stroke={colors.accent} strokeWidth="2" opacity="0.35" />
              <line x1="578" y1="200" x2="672" y2="200" stroke={colors.accent} strokeWidth="2" opacity="0.35" />
              <circle cx="588" cy="95" r="5" fill={colors.accent} opacity="0.4" />
            </g>
            {/* Connection */}
            <path d="M240 150 Q400 120 560 155" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.25" strokeDasharray="10 6" />
          </>
        );

      case 3: // Database doodle
        return (
          <>
            {/* DB 1 */}
            <g className="animate-[lift_4s_ease-in-out_infinite]">
              <ellipse cx="200" cy="90" rx="70" ry="22" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.5" />
              <path d="M130 90 L130 180 Q130 202 200 202 Q270 202 270 180 L270 90" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.5" />
              <ellipse cx="200" cy="180" rx="70" ry="22" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.35" />
              <ellipse cx="200" cy="120" rx="70" ry="22" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.25" strokeDasharray="5 3" />
              <ellipse cx="200" cy="150" rx="70" ry="22" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.2" strokeDasharray="5 3" />
            </g>
            {/* DB 2 */}
            <g className="animate-[lift_5s_ease-in-out_infinite]" style={{ animationDelay: '1.5s' }}>
              <ellipse cx="580" cy="100" rx="80" ry="25" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.45" />
              <path d="M500 100 L500 200 Q500 225 580 225 Q660 225 660 200 L660 100" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.45" />
              <ellipse cx="580" cy="200" rx="80" ry="25" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.3" />
              <ellipse cx="580" cy="133" rx="80" ry="25" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.2" strokeDasharray="5 3" />
              <ellipse cx="580" cy="166" rx="80" ry="25" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.15" strokeDasharray="5 3" />
            </g>
            {/* Sync arrow */}
            <path d="M280 140 Q400 100 490 130" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.25" strokeDasharray="8 6" />
            <path d="M480 125 L490 130 L480 138" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.25" />
          </>
        );

      case 4: // Wifi/Signal doodle
        return (
          <>
            {/* Router */}
            <g className="animate-[float_5s_ease-in-out_infinite]">
              <rect x="320" y="160" width="160" height="60" rx="6" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.5" />
              <circle cx="360" cy="190" r="8" fill={colors.accent} opacity="0.35" />
              <circle cx="400" cy="190" r="8" fill={colors.accent} opacity="0.25" />
              <circle cx="440" cy="190" r="8" fill={colors.accent} opacity="0.35" />
            </g>
            {/* Antenna */}
            <line x1="400" y1="160" x2="400" y2="110" stroke={colors.accent} strokeWidth="2" opacity="0.5" />
            <circle cx="400" cy="100" r="10" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.5" />
            {/* Signal waves */}
            <path d="M360 80 Q380 50 400 80" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.35" strokeLinecap="round" />
            <path d="M400 80 Q420 50 440 80" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.35" strokeLinecap="round" />
            <path d="M330 60 Q365 20 400 60" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.25" strokeLinecap="round" />
            <path d="M400 60 Q435 20 470 60" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.25" strokeLinecap="round" />
            <path d="M300 40 Q350 -10 400 40" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.15" strokeLinecap="round" />
            <path d="M400 40 Q450 -10 500 40" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.15" strokeLinecap="round" />
            {/* Devices */}
            <rect x="120" y="170" width="50" height="35" rx="4" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.35" />
            <rect x="630" y="170" width="50" height="35" rx="4" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.35" />
            <path d="M170 188 Q245 185 320 190" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.2" strokeDasharray="6 4" />
            <path d="M480 190 Q555 185 630 188" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.2" strokeDasharray="6 4" />
          </>
        );

      case 5: // Globe doodle
      default:
        return (
          <>
            {/* Globe */}
            <g className="animate-[float_6s_ease-in-out_infinite]">
              <circle cx="400" cy="150" r="80" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.5" />
              <ellipse cx="400" cy="150" rx="80" ry="30" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.3" />
              <ellipse cx="400" cy="150" rx="30" ry="80" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.3" />
              <line x1="400" y1="70" x2="400" y2="230" stroke={colors.accent} strokeWidth="1.5" opacity="0.2" />
              <line x1="320" y1="150" x2="480" y2="150" stroke={colors.accent} strokeWidth="1.5" opacity="0.2" />
            </g>
            {/* Orbit */}
            <ellipse cx="400" cy="150" rx="120" ry="40" fill="none" stroke={colors.accent} strokeWidth="2" opacity="0.25" strokeDasharray="10 6" className="animate-[spin_20s_linear_infinite]" style={{ transformOrigin: '400px 150px' }} />
            {/* Satellites */}
            <g className="animate-[float_4s_ease-in-out_infinite]">
              <rect x="180" y="100" width="30" height="20" rx="3" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.4" />
              <line x1="165" y1="110" x2="180" y2="110" stroke={colors.accent} strokeWidth="1.5" opacity="0.4" />
              <line x1="210" y1="110" x2="225" y2="110" stroke={colors.accent} strokeWidth="1.5" opacity="0.4" />
            </g>
            <g className="animate-[float_5s_ease-in-out_infinite]" style={{ animationDelay: '2s' }}>
              <rect x="590" y="180" width="30" height="20" rx="3" fill="none" stroke={colors.accent} strokeWidth="1.5" opacity="0.35" />
              <line x1="575" y1="190" x2="590" y2="190" stroke={colors.accent} strokeWidth="1.5" opacity="0.35" />
              <line x1="620" y1="190" x2="635" y2="190" stroke={colors.accent} strokeWidth="1.5" opacity="0.35" />
            </g>
            {/* Connection points */}
            <circle cx="350" cy="110" r="6" fill={colors.accent} opacity="0.3" />
            <circle cx="450" cy="190" r="6" fill={colors.accent} opacity="0.3" />
            <circle cx="380" cy="220" r="6" fill={colors.accent} opacity="0.3" />
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
