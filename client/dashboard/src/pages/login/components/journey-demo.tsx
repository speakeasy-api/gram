import { PlatformDiagram } from "./platform-diagram";

// Brand gradient colors for dotted background
const BRAND_COLORS = {
  blue: "#2873D7",
};

// Dotted background component
function DottedBackground() {
  return (
    <svg
      className="absolute inset-0 w-full h-full pointer-events-none"
      xmlns="http://www.w3.org/2000/svg"
    >
      <defs>
        <pattern
          id="login-dots-pattern"
          x="0"
          y="0"
          width="24"
          height="24"
          patternUnits="userSpaceOnUse"
        >
          <circle
            cx="12"
            cy="12"
            r="1"
            fill={BRAND_COLORS.blue}
            opacity="0.07"
          />
        </pattern>
      </defs>
      <rect width="100%" height="100%" fill="url(#login-dots-pattern)" />
    </svg>
  );
}

export function JourneyDemo() {
  return (
    <div className="hidden md:flex flex-col justify-center items-center w-full md:w-1/2 min-h-screen relative overflow-hidden bg-slate-50">
      {/* Dotted background pattern */}
      <DottedBackground />

      {/* Soft gradient overlays for depth */}
      <div className="absolute inset-0 bg-gradient-to-br from-blue-50/50 via-transparent to-emerald-50/30 pointer-events-none" />
      <div className="absolute inset-0 bg-gradient-to-t from-white/60 via-transparent to-white/40 pointer-events-none" />

      {/* Main platform diagram */}
      <PlatformDiagram className="relative z-10 w-full max-w-3xl px-6 scale-90 lg:scale-100" />

      {/* Bottom gradient fade */}
      <div className="absolute bottom-0 left-0 right-0 h-32 bg-gradient-to-t from-slate-50 to-transparent pointer-events-none" />
    </div>
  );
}
