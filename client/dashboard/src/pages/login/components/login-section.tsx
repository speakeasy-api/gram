"use client";

import { cn } from "@/lib/utils";
import { GramLogo } from "@/components/gram-logo/index";

const FEATURE_BADGES = ["Connect", "Secure", "Control", "Observe"];

function FeatureBadges({ labels = FEATURE_BADGES }: { labels?: string[] }) {
  return (
    <div className="flex justify-center gap-2">
      {labels.map((label) => (
        <span
          key={label}
          className="rounded-full border border-[#D3D3D3] px-3 py-1 font-mono text-xs tracking-[0.01em] text-[#8B8684] uppercase"
        >
          {label}
        </span>
      ))}
    </div>
  );
}

// Full-spectrum RGB gradient — Speakeasy brand signature element
function BrandGradientBar() {
  return (
    <div
      className="absolute right-0 bottom-0 left-0 h-[6px]"
      style={{
        background:
          "linear-gradient(90deg, #320F1E 0%, #C83228 12.5%, #FB873F 25%, #D2DC91 37.5%, #5A8250 50%, #002314 62%, #00143C 74%, #2873D7 86%, #9BC3FF 100%)",
      }}
    />
  );
}

// Moving dot background — same pattern as MCP cards, scrolls on hover
function DotBackground() {
  return (
    <>
      <style>{`
        @keyframes login-right-scroll-dots {
          from { background-position: 0 0; }
          to { background-position: 64px 64px; }
        }
        .login-right-pane:hover .login-right-dots {
          animation: login-right-scroll-dots 3s linear infinite;
        }
      `}</style>
      <div
        className="login-right-dots text-muted-foreground/10 pointer-events-none absolute inset-0"
        style={{
          backgroundImage:
            "radial-gradient(circle, currentColor 1px, transparent 1px)",
          backgroundSize: "16px 16px",
        }}
      />
    </>
  );
}

export function AuthLayout({
  children,
  topRight,
  contentClassName = "max-w-sm",
}: {
  children: React.ReactNode;
  topRight?: React.ReactNode;
  contentClassName?: string;
}): JSX.Element {
  return (
    <div className="login-right-pane relative flex min-h-screen w-full flex-col items-center justify-center overflow-hidden bg-[#FAFAFA] p-8 md:w-1/2 md:p-16">
      {/* Moving dot background — scrolls on hover */}
      <DotBackground />

      {topRight && (
        <div className="absolute top-6 right-6 z-10">{topRight}</div>
      )}

      <div
        className={cn(
          "relative z-10 flex w-full flex-col items-center gap-8",
          contentClassName,
        )}
      >
        <div className="flex flex-col items-center gap-4">
          <a
            href="https://www.speakeasy.com/product/mcp-platform"
            target="_blank"
            rel="noopener noreferrer"
          >
            <GramLogo
              className="mb-2 w-[200px] dark:!invert-0"
              variant="vertical"
            />
          </a>
          <div className="flex flex-col gap-2 text-center text-sm dark:text-black">
            <p>Securely scale AI usage across your organization.</p>
            <p className="text-[#8B8684]">
              Control plane to govern MCP, Skills, and Assistants
            </p>
          </div>
          <FeatureBadges />
        </div>

        {children}
      </div>

      <p className="absolute bottom-10 z-10 px-8 text-center text-[11px] text-[#8B8684]">
        By continuing, you agree to Speakeasy&apos;s{" "}
        <a
          href="https://www.speakeasy.com/terms-of-service"
          target="_blank"
          rel="noopener noreferrer"
          className="underline hover:text-slate-600"
        >
          Terms of Service
        </a>{" "}
        and{" "}
        <a
          href="https://www.speakeasy.com/privacy-policy"
          target="_blank"
          rel="noopener noreferrer"
          className="underline hover:text-slate-600"
        >
          Privacy Policy
        </a>
      </p>

      {/* Brand signature — RGB gradient bar at bottom edge */}
      <BrandGradientBar />
    </div>
  );
}
