import speakeasyIcon from "@/assets/speakeasy-icon.svg";
import { BrandGradientLine } from "@/components/brand-gradient-line";
import { AgentSessionShowcase } from "./agent-session-showcase";
import { SpeakeasyWordmark } from "./speakeasy-wordmark";
import { TermsFooter } from "./terms-footer";

// Speakeasy brand tokens from the design system (colors_and_type.css). The
// auth screens are a fixed light-mode brand surface, so these are scoped here
// rather than added to the app-wide theme. Chip colors come from the
// brandbook's language palette (moss, ember, vermilion, blue, navy).
const BRAND_STYLES = `
.auth-brand {
  --ink: rgb(15, 10, 7);
  --bone: rgb(250, 250, 250);
  --paper: rgb(255, 255, 255);
  --stone: rgb(87, 82, 81);
  --hairline: rgba(15, 10, 7, 0.12);
  --rule: rgba(15, 10, 7, 0.24);
  --moss: rgb(90, 130, 80);
  --ember: rgb(250, 135, 60);
  --vermilion: rgb(200, 50, 40);
  --blue: rgb(40, 115, 215);
  --navy: rgb(0, 20, 60);
  --f-display: "Tobias", "Times New Roman", serif;
  --f-sans: "Diatype", "Inter", system-ui, sans-serif;
  --f-mono: "Diatype Mono", ui-monospace, monospace;
  font-family: var(--f-sans);
  font-weight: 300;
}
.auth-mono {
  font-family: var(--f-mono);
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
.auth-mono-text {
  font-family: var(--f-mono);
}
@keyframes auth-live-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.25; }
}
.auth-live-dot {
  animation: auth-live-pulse 2s infinite;
}
@media (prefers-reduced-motion: reduce) {
  .auth-live-dot { animation: none; }
}
`;

function BrandLockup() {
  return (
    <div className="flex items-center gap-3.5">
      <img src={speakeasyIcon} alt="" className="h-10 w-10" />
      <SpeakeasyWordmark className="h-auto w-[210px]" />
    </div>
  );
}

export function AuthShell({
  page,
  children,
}: {
  page: "Login" | "Register";
  children: React.ReactNode;
}): JSX.Element {
  return (
    <main className="auth-brand flex min-h-screen flex-col bg-[var(--bone)] text-[var(--ink)]">
      <style>{BRAND_STYLES}</style>

      <header className="flex h-12 flex-none items-center justify-between border-b border-[var(--hairline)] px-6 md:px-12">
        <span className="auth-mono text-[13px]">
          Speakeasy AI Control Plane
        </span>
        <span className="auth-mono text-[13px] text-[var(--stone)]">
          {page}
        </span>
      </header>

      <div className="grid flex-1 xl:grid-cols-[1fr_560px]">
        <AgentSessionShowcase />

        <section className="relative flex flex-col items-center justify-center border-[var(--hairline)] bg-[var(--paper)] px-8 pt-16 pb-28 xl:border-l">
          <div className="flex w-full max-w-[360px] flex-col items-center gap-6">
            <BrandLockup />
            {children}
          </div>
          <TermsFooter
            className="absolute right-12 bottom-7 left-12 text-[var(--stone)]"
            linkClassName="text-[var(--stone)] hover:text-[var(--ink)]"
          />
          {/* .brand-gradient-line owns position:relative (its animated ::after
              needs it), so pin it via a wrapper instead of overriding. */}
          <div className="absolute inset-x-0 bottom-0">
            <BrandGradientLine className="h-[5px]" />
          </div>
        </section>
      </div>
    </main>
  );
}
