import { cn } from "@/lib/utils";

interface IllustrationProps {
  className?: string;
}

/**
 * Skeleton illustration of an OpenAPI document
 * Shows fake endpoints with HTTP methods
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
      <rect width="280" height="120" className="fill-blue-500/5" />

      {/* Header bar */}
      <rect x="16" y="12" width="80" height="8" rx="2" className="fill-blue-500/30" />
      <rect x="16" y="24" width="120" height="4" rx="1" className="fill-blue-500/15" />

      {/* Endpoint rows */}
      {/* GET endpoint */}
      <rect x="16" y="40" width="28" height="14" rx="3" className="fill-emerald-500/40" />
      <text x="22" y="50" className="fill-emerald-700 dark:fill-emerald-300 text-[8px] font-mono font-medium">GET</text>
      <rect x="50" y="44" width="90" height="6" rx="1" className="fill-blue-500/20" />

      {/* POST endpoint */}
      <rect x="16" y="60" width="28" height="14" rx="3" className="fill-amber-500/40" />
      <text x="19" y="70" className="fill-amber-700 dark:fill-amber-300 text-[8px] font-mono font-medium">POST</text>
      <rect x="50" y="64" width="70" height="6" rx="1" className="fill-blue-500/20" />

      {/* DELETE endpoint */}
      <rect x="16" y="80" width="28" height="14" rx="3" className="fill-red-500/40" />
      <text x="18" y="90" className="fill-red-700 dark:fill-red-300 text-[7px] font-mono font-medium">DEL</text>
      <rect x="50" y="84" width="60" height="6" rx="1" className="fill-blue-500/20" />

      {/* Right side - schema preview */}
      <rect x="160" y="40" width="100" height="54" rx="4" className="fill-blue-500/10 stroke-blue-500/20" strokeWidth="1" />
      <rect x="168" y="48" width="40" height="4" rx="1" className="fill-blue-500/25" />
      <rect x="168" y="56" width="60" height="3" rx="1" className="fill-blue-500/15" />
      <rect x="168" y="63" width="50" height="3" rx="1" className="fill-blue-500/15" />
      <rect x="168" y="70" width="70" height="3" rx="1" className="fill-blue-500/15" />
      <rect x="168" y="77" width="45" height="3" rx="1" className="fill-blue-500/15" />
      <rect x="168" y="84" width="55" height="3" rx="1" className="fill-blue-500/15" />
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
