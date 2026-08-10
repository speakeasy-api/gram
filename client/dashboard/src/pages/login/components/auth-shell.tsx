import speakeasyIcon from "@/assets/speakeasy-icon.svg";
import { BrandGradientLine } from "@/components/brand-gradient-line";
import { cn } from "@/lib/utils";
import { AgentSessionShowcase } from "./agent-session-showcase";
import { SpeakeasyWordmark } from "./speakeasy-wordmark";
import { TermsFooter } from "./terms-footer";

// Speakeasy brand tokens for the auth screens ("website styling" frames in the
// design project) — marketing-site neutrals plus the brandbook language
// palette for the session-card chips (moss, ember, vermilion, blue, navy).
// The auth screens are a fixed light-mode brand surface, so these are scoped
// here rather than added to the app-wide theme.
const BRAND_STYLES = `
.auth-brand {
  --surface: hsl(0, 0%, 98%);
  --card: #fff;
  --edge: hsl(0, 0%, 86%);
  --edge-soft: hsl(0, 0%, 92%);
  --muted: hsl(0, 0%, 46%);
  --muted-strong: hsl(0, 0%, 33%);
  --cta: hsl(0, 0%, 20%);
  --cta-hover: hsl(0, 0%, 14%);
  --link: hsl(201, 86%, 37%);
  --focus: hsl(217, 91%, 60%);
  --input-edge: hsl(240, 5%, 84%);
  --vermilion: hsl(4, 67%, 47%);
  --moss: rgb(90, 130, 80);
  --ember: rgb(250, 135, 60);
  --blue: rgb(40, 115, 215);
  --navy: rgb(0, 20, 60);
  --f-display: "Tobias", "Times New Roman", serif;
  --f-sans: "Diatype", "Inter", system-ui, sans-serif;
  --f-mono: "Diatype Mono", ui-monospace, monospace;
  font-family: var(--f-sans);
  font-weight: 400;
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
/* The showcase is designed against a 900px-tall frame; on shorter viewports
   (laptop screens minus browser chrome) zoom the stage down so the card and
   the social links always fit above the fold. */
@media (max-height: 860px) {
  .auth-showcase { padding-top: 24px; padding-bottom: 48px; }
  .auth-showcase-stage { zoom: 0.8; }
}
@media (max-height: 700px) {
  .auth-showcase-stage { zoom: 0.65; }
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
  headerAction,
  contentClassName,
  showTerms = true,
  children,
}: {
  page: string;
  /** Extra control on the right of the header, e.g. a Log out link. */
  headerAction?: React.ReactNode;
  /** Overrides the right-pane column width (default max-w-[380px]). */
  contentClassName?: string;
  showTerms?: boolean;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <main className="auth-brand flex min-h-screen flex-col bg-[var(--surface)] text-black">
      <style>{BRAND_STYLES}</style>

      <BrandGradientLine className="h-[4px]" />

      <header className="flex h-16 flex-none items-center justify-between border-b border-[var(--edge)] bg-[var(--card)] px-6 md:px-10">
        <span className="auth-mono flex items-center gap-3 text-[13px]">
          <img src={speakeasyIcon} alt="" className="h-[22px] w-[22px]" />
          Speakeasy AI Control Plane
        </span>
        <span className="flex items-center gap-6">
          <span className="auth-mono text-[13px] text-[var(--muted)]">
            {page}
          </span>
          {headerAction}
        </span>
      </header>

      <div className="grid flex-1 xl:grid-cols-2">
        <AgentSessionShowcase />

        {/* The deep bottom padding only exists to clear the absolutely
            positioned terms footer; without it that space is dead. */}
        <section
          className={cn(
            "relative flex flex-col items-center justify-center border-[var(--edge-soft)] bg-[var(--card)] px-8 pt-16 xl:border-l",
            showTerms ? "pb-28" : "pb-12",
          )}
        >
          <div
            className={cn(
              "flex w-full max-w-[380px] flex-col items-center gap-6",
              contentClassName,
            )}
          >
            <BrandLockup />
            {children}
          </div>
          {showTerms && (
            <TermsFooter
              className="absolute right-12 bottom-7 left-12 text-[var(--muted-strong)]"
              linkClassName="text-[var(--muted-strong)] hover:text-black"
            />
          )}
        </section>
      </div>
    </main>
  );
}
