import { useEffect, useState } from "react";

// Animated walkthrough of one governed agent session: five policy decisions
// light up in sequence on a ~22s loop, mirroring the product's decision log.
type DecisionStep = {
  label: string;
  chip: string;
  chipClassName: string;
  detail: string;
};

const DECISION_STEPS: DecisionStep[] = [
  {
    label: "Connect billing-api MCP",
    chip: "Granted",
    chipClassName: "bg-[var(--moss)] text-[var(--bone)]",
    detail: "401 → OAuth via Okta · sarah@acme.com",
  },
  {
    label: "get_invoice INV-4821",
    chip: "Fields masked",
    chipClassName: "bg-[var(--ember)] text-[var(--ink)]",
    detail: "card, email redacted for the model",
  },
  {
    label: "create_refund $1,200",
    chip: "Denied",
    chipClassName: "bg-[var(--vermilion)] text-[var(--bone)]",
    detail: "destructive · deny wins over team allow",
  },
  {
    label: "void_invoice — 3 duplicates",
    chip: "Held",
    chipClassName: "bg-[var(--blue)] text-[var(--bone)]",
    detail: "llm-as-judge: held for human approval",
  },
  {
    label: "Post summary to #billing-ops",
    chip: "Granted",
    chipClassName: "bg-[var(--moss)] text-[var(--bone)]",
    detail: "audit written · session cost $0.05",
  },
];

// Second marks at which each decision row activates, on a loop of LOOP_SECONDS.
const STEP_STARTS = [1, 5, 9, 13, 17];
const LOOP_SECONDS = 22;
const TICK_MS = 100;
const SPEED = 1.25;
// Chip + detail fade in this long after their row activates.
const REVEAL_DELAY_SECONDS = 1.6;
const SESSION_COSTS = ["$0.00", "$0.01", "$0.02", "$0.02", "$0.03", "$0.05"];

function useSessionClock(): number {
  // With reduced motion the clock stays parked past the last reveal, so the
  // card renders as a completed session instead of looping.
  const [elapsed, setElapsed] = useState(LOOP_SECONDS - 1);

  useEffect(() => {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
      return;
    }
    setElapsed(0);
    const timer = setInterval(() => {
      setElapsed((prev) => {
        const next = prev + (TICK_MS / 1000) * SPEED;
        return next >= LOOP_SECONDS ? 0 : next;
      });
    }, TICK_MS);
    return () => clearInterval(timer);
  }, []);

  return elapsed;
}

function activeStep(elapsed: number): number {
  return STEP_STARTS.reduce(
    (acc, start, index) => (elapsed >= start ? index : acc),
    -1,
  );
}

function rowOpacity(index: number, step: number): number {
  if (step < 0) return 0.35;
  if (index === step) return 1;
  return index < step ? 0.6 : 0.3;
}

function revealOpacity(index: number, elapsed: number): number {
  const start = STEP_STARTS[index] ?? 0;
  return elapsed >= start + REVEAL_DELAY_SECONDS ? 1 : 0;
}

function DecisionRow({
  step,
  index,
  elapsed,
  currentStep,
}: {
  step: DecisionStep;
  index: number;
  elapsed: number;
  currentStep: number;
}) {
  const reveal = revealOpacity(index, elapsed);
  return (
    <div
      className="border border-[var(--hairline)] px-3 py-[9px] transition-opacity duration-500"
      style={{ opacity: rowOpacity(index, currentStep) }}
    >
      <div className="flex items-center justify-between gap-2.5">
        <span className="text-[14px]">{step.label}</span>
        <span
          className={`auth-mono px-2 py-[3px] text-[10px] transition-opacity duration-400 ${step.chipClassName}`}
          style={{ opacity: reveal }}
        >
          {step.chip}
        </span>
      </div>
      <div
        className="auth-mono-text mt-[5px] text-[12px] text-[var(--stone)] transition-opacity duration-400"
        style={{ opacity: reveal }}
      >
        {step.detail}
      </div>
    </div>
  );
}

function AgentSessionCard({ elapsed }: { elapsed: number }) {
  const currentStep = activeStep(elapsed);
  const cost = SESSION_COSTS[currentStep + 1];
  const progress = Math.min(100, (elapsed / (LOOP_SECONDS - 0.5)) * 100);

  return (
    <div className="w-[540px] border border-[var(--hairline)] bg-[var(--paper)]">
      <div className="flex h-11 items-center justify-between border-b border-[var(--hairline)] px-5">
        <span className="auth-mono flex items-center gap-2.5 text-[12px]">
          <i className="auth-live-dot h-[7px] w-[7px] rounded-full bg-[var(--moss)]" />
          Agent session
        </span>
        <span className="auth-mono text-[12px] text-[var(--stone)]">Live</span>
      </div>

      <div className="flex gap-3 px-5 pt-4 pb-1.5">
        <span className="flex h-6 w-6 flex-none items-center justify-center rounded-full bg-[var(--navy)] text-[11px] text-[var(--bone)]">
          S
        </span>
        <p className="mt-0.5 text-[15px] leading-normal">
          Handle the refund request on order #4821 — verify the invoice and
          process it if valid.
        </p>
      </div>

      <div className="flex flex-col gap-[7px] px-5 pt-3 pb-4">
        {DECISION_STEPS.map((step, index) => (
          <DecisionRow
            key={step.label}
            step={step}
            index={index}
            elapsed={elapsed}
            currentStep={currentStep}
          />
        ))}
      </div>

      <div className="auth-mono-text flex justify-between px-5 pb-3.5 text-[11px] tracking-[0.04em] text-[var(--stone)]">
        <span>task: refund-request-4821</span>
        <span>session cost {cost}</span>
      </div>

      <div className="h-[3px] bg-[var(--hairline)]">
        <div
          className="h-full bg-[var(--ink)] transition-[width] duration-100 ease-linear"
          style={{ width: `${progress}%` }}
        />
      </div>
    </div>
  );
}

const SOCIAL_LINKS = [
  { label: "X", href: "https://x.com/speakeasydev" },
  { label: "GitHub", href: "https://github.com/speakeasy-api/gram" },
  { label: "Docs", href: "https://www.speakeasy.com/docs/mcp" },
];

export function AgentSessionShowcase(): JSX.Element {
  const elapsed = useSessionClock();

  return (
    <div className="relative hidden flex-col items-center justify-center gap-9 p-12 xl:flex">
      <h2 className="w-[540px] [font-family:var(--f-display)] text-[52px] leading-[0.92] font-thin tracking-[-0.03em]">
        Every agent action, governed.
      </h2>

      <div aria-hidden="true">
        <AgentSessionCard elapsed={elapsed} />
      </div>

      <div className="absolute right-0 bottom-5 left-0 flex justify-center gap-7">
        {SOCIAL_LINKS.map((link) => (
          <a
            key={link.label}
            href={link.href}
            target="_blank"
            rel="noopener noreferrer"
            className="auth-mono text-[12px] tracking-[0.06em] text-[var(--stone)] transition-colors hover:text-[var(--ink)]"
          >
            {link.label}
          </a>
        ))}
      </div>
    </div>
  );
}
