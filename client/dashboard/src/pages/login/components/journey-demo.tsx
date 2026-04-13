import { PlatformDiagram } from "./platform-diagram";

// Dot background — scrolls on hover, matching MCP card pattern
function MovingDotBackground() {
  return (
    <>
      <style>{`
        @keyframes login-scroll-dots {
          from { background-position: 0 0; }
          to { background-position: 64px 64px; }
        }
        .login-pane:hover .login-dots {
          animation: login-scroll-dots 3s linear infinite;
        }
      `}</style>
      <div
        className="login-dots text-muted-foreground/20 pointer-events-none absolute inset-0"
        style={{
          backgroundImage:
            "radial-gradient(circle, currentColor 1px, transparent 1px)",
          backgroundSize: "16px 16px",
        }}
      />
    </>
  );
}

export function JourneyDemo() {
  return (
    <div className="login-pane relative hidden min-h-screen w-full flex-col items-center justify-center overflow-y-auto bg-slate-50 md:flex md:w-1/2">
      {/* Moving dot background — same pattern as MCP cards */}
      <MovingDotBackground />

      {/* Soft gradient overlays for depth */}
      <div className="pointer-events-none absolute inset-0 bg-gradient-to-br from-blue-50/50 via-transparent to-emerald-50/30" />
      <div className="pointer-events-none absolute inset-0 bg-gradient-to-t from-white/60 via-transparent to-white/40" />

      {/* Main platform diagram */}
      <PlatformDiagram className="relative z-10 w-full px-8 py-12" />

      {/* Top/Bottom gradient fades */}
      <div className="pointer-events-none absolute top-0 right-0 left-0 z-20 h-16 bg-gradient-to-b from-slate-50 to-transparent" />
      <div className="pointer-events-none absolute right-0 bottom-0 left-0 z-20 h-16 bg-gradient-to-t from-slate-50 to-transparent" />

      {/* Fixed bottom social links */}
      <div className="absolute right-0 bottom-6 left-0 z-30 flex items-center justify-center gap-4">
        <a
          href="https://x.com/speakeasydev"
          target="_blank"
          rel="noopener noreferrer"
          className="text-slate-400 transition-colors hover:text-slate-600"
          aria-label="Follow us on X"
        >
          <svg className="h-5 w-5" viewBox="0 0 24 24" fill="currentColor">
            <path d="M18.244 2.25h3.308l-7.227 8.26 8.502 11.24H16.17l-5.214-6.817L4.99 21.75H1.68l7.73-8.835L1.254 2.25H8.08l4.713 6.231zm-1.161 17.52h1.833L7.084 4.126H5.117z" />
          </svg>
        </a>
        <a
          href="https://github.com/speakeasy-api/gram"
          target="_blank"
          rel="noopener noreferrer"
          className="text-slate-400 transition-colors hover:text-slate-600"
          aria-label="View on GitHub"
        >
          <svg className="h-5 w-5" viewBox="0 0 24 24" fill="currentColor">
            <path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0024 12c0-6.63-5.37-12-12-12z" />
          </svg>
        </a>
        <a
          href="https://www.speakeasy.com/docs/mcp"
          target="_blank"
          rel="noopener noreferrer"
          className="text-slate-400 transition-colors hover:text-slate-600"
          aria-label="Documentation"
        >
          <svg
            className="h-5 w-5"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <path d="M4 19.5A2.5 2.5 0 016.5 17H20" />
            <path d="M6.5 2H20v20H6.5A2.5 2.5 0 014 19.5v-15A2.5 2.5 0 016.5 2z" />
          </svg>
        </a>
      </div>
    </div>
  );
}
