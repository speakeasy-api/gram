import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { telemetryQuery } from "@gram/client/funcs/telemetryQuery";
import type { QueryResult } from "@gram/client/models/components/queryresult.js";
import { useDeviceIntegrationCoverage } from "@gram/client/react-query/deviceIntegrationCoverage.js";
import { unwrapAsync } from "@gram/client/types/fp.js";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/Badge";
import { InternalAdminBadge } from "@/components/internal-admin-badge";
import { cn } from "@/lib/utils";
import { PlatformAdminGate } from "./PlatformAdminGate";
import {
  capabilities,
  defaultPlannerState,
  methods,
  outcomes,
  recommendation,
  surfaceForHookSource,
  surfaces,
  targetCapabilities,
  type CapabilityId,
  type OutcomeId,
  type PlannerState,
  type SurfaceId,
} from "./support-matrix-model";

const WINDOW_DAYS = 30;

type SurfaceEvidence = {
  sessions: number;
  tokens: number;
  lastSeen: Date | null;
};

type EvidenceStatus = "loading" | "unavailable" | "ready";

export default function SupportMatrix(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <PlatformAdminGate>
          <Planner />
        </PlatformAdminGate>
      </Page.Body>
    </Page>
  );
}

function Planner(): JSX.Element {
  const organization = useOrganization();
  return <OrganizationPlanner key={organization.id} />;
}

function OrganizationPlanner(): JSX.Element {
  const organization = useOrganization();
  const client = useSdkClient();
  const storageKey = `gram-support-matrix:${organization.id}`;
  const [state, setState] = useState<PlannerState>(() => readState(storageKey));
  useEffect(() => {
    try {
      sessionStorage.setItem(storageKey, JSON.stringify(state));
    } catch {
      /* Session persistence is best-effort. */
    }
  }, [state, storageKey]);

  const from = useMemo(
    () => new Date(Date.now() - WINDOW_DAYS * 86_400_000),
    [],
  );
  const to = useMemo(() => new Date(), []);
  const telemetry = useQuery({
    queryKey: [
      "support-coverage",
      organization.id,
      from.toISOString(),
      to.toISOString(),
    ],
    queryFn: () =>
      unwrapAsync(
        telemetryQuery(client, {
          queryPayload: {
            from,
            to,
            groupBy: "hook_source",
            sortBy: "total_chats",
            topN: 1000,
            granularitySeconds: 86_400,
          },
        }),
      ),
    staleTime: 60_000,
    throwOnError: false,
  });
  const deviceCoverage = useDeviceIntegrationCoverage(undefined, undefined, {
    staleTime: 60_000,
    throwOnError: false,
  });
  const evidence = useMemo(
    () => buildEvidence(telemetry.data),
    [telemetry.data],
  );
  const recommended = recommendation(state);

  const update = (patch: Partial<PlannerState>) =>
    setState((current) => ({ ...current, ...patch }));
  const toggle = <T extends string>(values: T[], value: T): T[] =>
    values.includes(value)
      ? values.filter((item) => item !== value)
      : [...values, value];

  return (
    <div className="min-h-[calc(100vh-8rem)]">
      <div className="border-border flex flex-wrap items-center justify-between gap-3 border-b py-2">
        <div className="flex items-center gap-2">
          <InternalAdminBadge />
          <Badge variant="neutral">
            <Badge.Text>{organization.name}</Badge.Text>
          </Badge>
        </div>
        <span className="text-muted-foreground font-mono text-[10px] tracking-[0.08em] uppercase">
          Onboarding · {state.step} of 4 · Session only · {WINDOW_DAYS} days
        </span>
      </div>
      <StepRail step={state.step} onStep={(step) => update({ step })} />

      {state.step === 1 && (
        <OutcomeStep
          selected={state.outcomes}
          onToggle={(id) => update({ outcomes: toggle(state.outcomes, id) })}
          onNext={() => update({ step: 2 })}
        />
      )}
      {state.step === 2 && (
        <SurfaceStep
          selected={state.surfaces}
          onToggle={(id) => update({ surfaces: toggle(state.surfaces, id) })}
          onBack={() => update({ step: 1 })}
          onNext={() => update({ step: 3 })}
        />
      )}
      {state.step === 3 && <SetupStep state={state} update={update} />}
      {state.step === 4 && (
        <Results
          state={state}
          evidence={evidence}
          recommended={recommended}
          telemetryStatus={
            telemetry.isPending
              ? "loading"
              : telemetry.isError
                ? "unavailable"
                : "ready"
          }
          deviceStatus={
            deviceCoverage.isPending
              ? "loading"
              : deviceCoverage.isError
                ? "unavailable"
                : "ready"
          }
          activeAgents={deviceCoverage.data?.agentActive ?? 0}
          activeWindowMinutes={deviceCoverage.data?.activeWindowMinutes}
          onBack={() => update({ step: 3 })}
          onReset={() => {
            try {
              sessionStorage.removeItem(storageKey);
            } catch {
              /* noop */
            }
            setState(defaultPlannerState);
          }}
        />
      )}
    </div>
  );
}

function StepRail({
  step,
  onStep,
}: {
  step: PlannerState["step"];
  onStep: (step: PlannerState["step"]) => void;
}) {
  return (
    <div className="mx-auto grid max-w-[1120px] grid-cols-4 gap-px pt-3">
      {[1, 2, 3, 4].map((value) => (
        <button
          key={value}
          type="button"
          onClick={() => onStep(value as PlannerState["step"])}
          disabled={value > step}
          className={cn(
            "h-px disabled:cursor-default",
            value <= step ? "bg-[#2873D7]" : "bg-border",
          )}
          aria-label={`Go to step ${value}`}
          aria-current={value === step ? "step" : undefined}
        />
      ))}
    </div>
  );
}

function StepHeading({
  step,
  title,
  description,
}: {
  step: string;
  title: string;
  description: string;
}) {
  return (
    <div>
      <p className="text-muted-foreground font-mono text-[11px] tracking-[0.1em] uppercase">
        Step {step} / 04
      </p>
      <h2 className="mt-3 max-w-4xl [font-family:var(--f-display)] text-[clamp(2.75rem,6vw,4rem)] leading-[0.98] font-thin tracking-[-0.045em]">
        {title}
      </h2>
      <div className="mt-4 h-0.5 w-44 bg-gradient-to-r from-[#2873D7] to-[#8BD4FF]" />
      <p className="text-muted-foreground mt-3 max-w-2xl text-lg leading-relaxed">
        {description}
      </p>
    </div>
  );
}

function OutcomeStep({
  selected,
  onToggle,
  onNext,
}: {
  selected: OutcomeId[];
  onToggle: (id: OutcomeId) => void;
  onNext: () => void;
}) {
  const [focused, setFocused] = useState<OutcomeId | null>(null);
  const previewId = focused ?? selected[selected.length - 1] ?? "security";
  const preview = outcomes.find((outcome) => outcome.id === previewId)!;

  return (
    <main className="mx-auto max-w-[1120px] py-7 sm:py-9">
      <StepHeading
        step="01"
        title="What do you want to do?"
        description="Choose the outcomes that matter for this organization. We’ll use them to rank the strongest integration path."
      />
      <div className="mt-7 grid gap-7 lg:grid-cols-[380px_minmax(0,1fr)]">
        <div className="space-y-2">
          {outcomes.map((item) => {
            const isSelected = selected.includes(item.id);
            return (
              <button
                key={item.id}
                type="button"
                onClick={() => onToggle(item.id)}
                onMouseEnter={() => setFocused(item.id)}
                onMouseLeave={() => setFocused(null)}
                onFocus={() => setFocused(item.id)}
                onBlur={() => setFocused(null)}
                aria-pressed={isSelected}
                className={cn(
                  "group w-full border bg-white p-3.5 text-left transition-colors dark:bg-background",
                  isSelected
                    ? "border-[#2873D7]"
                    : "border-border hover:border-foreground/40",
                )}
              >
                <span className="flex items-start gap-4">
                  <span
                    className={cn(
                      "mt-1 size-4 shrink-0 border",
                      isSelected
                        ? "border-[#2873D7] bg-[#2873D7]"
                        : "border-foreground/35",
                    )}
                  />
                  <span>
                    <span className="block text-[17px] font-medium">
                      {item.name}
                    </span>
                    <span className="text-muted-foreground mt-0.5 block text-[13px] leading-snug">
                      {item.description}
                    </span>
                  </span>
                </span>
              </button>
            );
          })}
          <div className="flex items-center justify-between gap-4 pt-2">
            <span className="text-muted-foreground font-mono text-[10px] tracking-[0.08em] uppercase">
              {selected.length} selected
            </span>
            <button
              type="button"
              onClick={onNext}
              disabled={selected.length === 0}
              className="bg-foreground px-6 py-3 font-mono text-xs tracking-[0.08em] text-background uppercase transition-colors hover:bg-[#2873D7] disabled:cursor-not-allowed disabled:opacity-40"
            >
              Continue →
            </button>
          </div>
        </div>
        <OutcomePreview
          id={previewId}
          name={preview.name}
          description={preview.description}
        />
      </div>
    </main>
  );
}

function OutcomePreview({
  id,
  name,
  description,
}: {
  id: OutcomeId;
  name: string;
  description: string;
}) {
  const figure = { gateway: "01", security: "02", cost: "03", identity: "04" }[
    id
  ];
  const labels =
    id === "gateway"
      ? ["Agents", "Gateway", "MCP servers"]
      : id === "security"
        ? ["Prompt", "Policy", "Tool call"]
        : id === "cost"
          ? ["Sessions", "Usage", "Spend"]
          : ["Identity", "Role", "Agent activity"];
  return (
    <aside className="border-border flex min-h-[380px] flex-col border bg-[#F7F8FA] p-6 dark:bg-muted/20">
      <p className="text-muted-foreground font-mono text-[10px] tracking-[0.1em] uppercase">
        Fig. {figure} — {name}
      </p>
      <div
        className="flex flex-1 items-center justify-center py-6"
        aria-hidden="true"
      >
        <div className="flex w-full max-w-xl items-center">
          {labels.map((label, index) => (
            <div key={label} className="contents">
              <div
                className={cn(
                  "flex min-h-20 min-w-0 flex-1 items-center justify-center border px-2 text-center font-mono text-[10px] tracking-[0.06em] uppercase",
                  index === 1
                    ? "border-[#2873D7] bg-[#2873D7] text-white"
                    : "border-foreground/20 bg-background",
                )}
              >
                {label}
              </div>
              {index < labels.length - 1 && (
                <div className="bg-foreground/25 relative h-px w-5 shrink-0 sm:w-9">
                  <span className="absolute -top-[3px] right-0 size-1.5 rotate-45 border-t border-r border-foreground/40" />
                </div>
              )}
            </div>
          ))}
        </div>
      </div>
      <div className="border-border border-t pt-5">
        <p className="text-lg font-medium">{name}</p>
        <p className="text-muted-foreground mt-2 max-w-xl leading-relaxed">
          {description}
        </p>
        <p className="text-muted-foreground mt-5 font-mono text-[10px] tracking-[0.08em] uppercase">
          Recommendation input · no configuration changes
        </p>
      </div>
    </aside>
  );
}

function SurfaceStep({
  selected,
  onToggle,
  onBack,
  onNext,
}: {
  selected: SurfaceId[];
  onToggle: (id: SurfaceId) => void;
  onBack: () => void;
  onNext: () => void;
}) {
  return (
    <main className="mx-auto max-w-[980px] py-10 sm:py-14">
      <StepHeading
        step="02"
        title="Where do your agents work?"
        description="Coverage is evaluated per surface. A single product can need different integrations across CLI, desktop, and cloud."
      />
      <div className="mt-10 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {surfaces.map((surface) => {
          const isSelected = selected.includes(surface.id);
          return (
            <button
              key={surface.id}
              type="button"
              onClick={() => onToggle(surface.id)}
              aria-pressed={isSelected}
              className={cn(
                "min-h-32 border bg-white p-5 text-left transition-colors dark:bg-background",
                isSelected
                  ? "border-[#2873D7] shadow-[inset_0_0_0_1px_#2873D7]"
                  : "border-border hover:border-foreground/40",
              )}
            >
              <span className="flex items-start justify-between gap-4">
                <span className="text-lg font-medium">{surface.name}</span>
                <span
                  className={cn(
                    "size-4 shrink-0 border",
                    isSelected
                      ? "border-[#2873D7] bg-[#2873D7]"
                      : "border-foreground/30",
                  )}
                />
              </span>
              <span className="text-muted-foreground mt-8 block font-mono text-[10px] tracking-[0.06em] uppercase">
                {surface.detail}
              </span>
            </button>
          );
        })}
      </div>
      <StepActions
        onBack={onBack}
        onNext={onNext}
        nextDisabled={selected.length === 0}
      />
    </main>
  );
}

function StepActions({
  onBack,
  onNext,
  nextDisabled = false,
  nextLabel = "Continue →",
}: {
  onBack: () => void;
  onNext: () => void;
  nextDisabled?: boolean;
  nextLabel?: string;
}) {
  return (
    <div className="mt-8 flex items-center gap-5">
      <button
        type="button"
        onClick={onBack}
        className="text-muted-foreground font-mono text-xs tracking-[0.08em] uppercase hover:text-foreground"
      >
        ← Back
      </button>
      <button
        type="button"
        onClick={onNext}
        disabled={nextDisabled}
        className="bg-foreground px-6 py-3 font-mono text-xs tracking-[0.08em] text-background uppercase transition-colors hover:bg-[#2873D7] disabled:opacity-40"
      >
        {nextLabel}
      </button>
    </div>
  );
}

function SetupStep({
  state,
  update,
}: {
  state: PlannerState;
  update: (patch: Partial<PlannerState>) => void;
}) {
  const questions = [
    {
      id: "plan",
      title: "Which Claude plan is in use?",
      options: ["Enterprise", "Team", "Individual / unknown"],
    },
    {
      id: "mdm",
      title: "How are developer devices managed?",
      options: ["Managed fleet", "Some managed devices", "No MDM"],
    },
  ];
  return (
    <main className="mx-auto max-w-[980px] py-10 sm:py-14">
      <StepHeading
        step="03"
        title="A few details about the setup"
        description="These answers shape eligibility only. They remain in this browser tab and do not change the organization’s configuration."
      />
      <div className="divide-border mt-10 divide-y border-y">
        {questions.map((question, index) => (
          <div
            key={question.id}
            className="grid gap-5 py-8 md:grid-cols-[minmax(0,1fr)_minmax(0,1.35fr)] md:items-center"
          >
            <div>
              <p className="text-muted-foreground font-mono text-[10px] tracking-[0.08em] uppercase">
                Question 0{index + 1}
              </p>
              <p
                id={`${question.id}-label`}
                className="mt-2 text-lg font-medium"
              >
                {question.title}
              </p>
            </div>
            <div
              className="flex flex-wrap gap-2 md:justify-end"
              role="group"
              aria-labelledby={`${question.id}-label`}
            >
              {question.options.map((option) => {
                const isSelected = state.answers[question.id] === option;
                return (
                  <button
                    key={option}
                    type="button"
                    aria-pressed={isSelected}
                    onClick={() =>
                      update({
                        answers: { ...state.answers, [question.id]: option },
                      })
                    }
                    className={cn(
                      "border px-4 py-2.5 font-mono text-[11px] tracking-[0.04em] uppercase transition-colors",
                      isSelected
                        ? "border-foreground bg-foreground text-background"
                        : "border-border bg-background hover:border-foreground/50",
                    )}
                  >
                    {option}
                  </button>
                );
              })}
            </div>
          </div>
        ))}
      </div>
      <StepActions
        onBack={() => update({ step: 2 })}
        onNext={() => update({ step: 4 })}
        nextLabel="Build coverage plan →"
      />
    </main>
  );
}

function Results({
  state,
  evidence,
  recommended,
  telemetryStatus,
  deviceStatus,
  activeAgents,
  activeWindowMinutes,
  onBack,
  onReset,
}: {
  state: PlannerState;
  evidence: Map<SurfaceId, SurfaceEvidence>;
  recommended: ReturnType<typeof recommendation>;
  telemetryStatus: EvidenceStatus;
  deviceStatus: EvidenceStatus;
  activeAgents: number;
  activeWindowMinutes?: number;
  onBack: () => void;
  onReset: () => void;
}) {
  const wanted = targetCapabilities(state.outcomes);
  const targetCells = state.surfaces.length * wanted.length;
  const recommendationCells = recommended
    ? recommended.surfaces.filter((s) => state.surfaces.includes(s)).length *
      recommended.capabilities.filter((c) => wanted.includes(c)).length
    : 0;
  return (
    <main className="mx-auto max-w-[1120px] space-y-14 py-10 sm:py-14">
      <StepHeading
        step="04"
        title="Your first coverage move"
        description="A focused starting point based on the selected outcomes, surfaces, and environment constraints."
      />
      <section className="border-border grid border bg-white dark:bg-background lg:grid-cols-[1.05fr_0.95fr]">
        <div className="p-6 sm:p-10">
          <p className="font-mono text-[10px] tracking-[0.1em] text-[#2873D7] uppercase">
            Recommended first integration
          </p>
          <h3 className="mt-5 [font-family:var(--f-display)] text-[clamp(2.5rem,5vw,3.5rem)] leading-none font-thin tracking-[-0.04em]">
            {recommended?.name ?? "No eligible integration"}
          </h3>
          <p className="text-muted-foreground mt-5 max-w-xl text-lg leading-relaxed">
            {recommended?.description ??
              "Adjust the setup answers or selected surfaces to see a recommendation."}
          </p>
          {recommended && (
            <div className="border-border mt-8 grid gap-6 border-y py-6 sm:grid-cols-3">
              <Metric
                value={`${recommendationCells}/${targetCells}`}
                label="target cells"
              />
              <Metric value={recommended.setup} label="estimated setup" />
              <Metric
                value={
                  deviceStatus === "loading"
                    ? "Loading…"
                    : deviceStatus === "unavailable"
                      ? "Unavailable"
                      : String(activeAgents)
                }
                label={
                  activeWindowMinutes
                    ? `agents active within ${activeWindowMinutes} min`
                    : "active device agents"
                }
              />
            </div>
          )}
          <div className="mt-8 flex gap-5">
            <button
              type="button"
              onClick={onBack}
              className="text-muted-foreground font-mono text-xs tracking-[0.08em] uppercase hover:text-foreground"
            >
              ← Back
            </button>
            <button
              type="button"
              onClick={onReset}
              className="font-mono text-xs tracking-[0.08em] uppercase hover:text-[#2873D7]"
            >
              Start over
            </button>
          </div>
        </div>
        <ProjectedCoverage state={state} recommended={recommended} />
      </section>
      <section className="space-y-4">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <p className="text-muted-foreground font-mono text-[10px] tracking-[0.1em] uppercase">
              Evidence layer
            </p>
            <h3 className="mt-2 [font-family:var(--f-display)] text-4xl font-thin tracking-[-0.035em]">
              Observed coverage
            </h3>
            <p className="text-muted-foreground mt-2 text-sm">
              Actual aggregate telemetry for this organization. Empty cells are
              unknown—not proof that a capability is absent.
            </p>
          </div>
          {telemetryStatus === "loading" ? (
            <Badge variant="neutral">
              <Badge.Text>Loading evidence…</Badge.Text>
            </Badge>
          ) : telemetryStatus === "unavailable" ? (
            <Badge variant="warning">
              <Badge.Text>Evidence unavailable</Badge.Text>
            </Badge>
          ) : (
            <Badge variant="success">
              <Badge.Text>Last 30 days</Badge.Text>
            </Badge>
          )}
        </div>
        <CoverageTable
          state={state}
          evidence={evidence}
          telemetryStatus={telemetryStatus}
        />
      </section>
      <section className="space-y-4">
        <div>
          <p className="text-muted-foreground font-mono text-[10px] tracking-[0.1em] uppercase">
            Integration footprints
          </p>
          <h3 className="mt-2 [font-family:var(--f-display)] text-4xl font-thin tracking-[-0.035em]">
            Other paths
          </h3>
        </div>
        <div className="grid gap-3 md:grid-cols-2">
          {methods
            .filter(
              (method) => !method.eligible || method.eligible(state.answers),
            )
            .map((method) => (
              <div
                key={method.id}
                className={cn(
                  "border-border bg-card border p-5",
                  method.id === recommended?.id &&
                    "border-[#2873D7] shadow-[inset_3px_0_0_#2873D7]",
                )}
              >
                <div className="flex items-center justify-between gap-2">
                  <p className="font-medium">{method.name}</p>
                  {method.id === recommended?.id && (
                    <Badge variant="information">
                      <Badge.Text>Recommended</Badge.Text>
                    </Badge>
                  )}
                </div>
                <p className="text-muted-foreground mt-2 text-sm">
                  {method.description}
                </p>
                <p className="text-muted-foreground mt-3 font-mono text-xs">
                  {method.setup}
                </p>
              </div>
            ))}
        </div>
      </section>
    </main>
  );
}

function ProjectedCoverage({
  state,
  recommended,
}: {
  state: PlannerState;
  recommended: ReturnType<typeof recommendation>;
}) {
  const wanted = targetCapabilities(state.outcomes);
  return (
    <div className="border-border bg-[#F7F8FA] p-6 sm:p-10 lg:border-l dark:bg-muted/20">
      <p className="font-mono text-[10px] tracking-[0.1em] uppercase">
        Projected coverage
      </p>
      <p className="text-muted-foreground mt-2 text-sm leading-relaxed">
        Static capability fit for the recommendation—not observed activity.
      </p>
      <div className="mt-8 overflow-x-auto">
        <div
          className="grid min-w-[420px] gap-px bg-foreground/10"
          style={{
            gridTemplateColumns: `120px repeat(${state.surfaces.length}, minmax(56px, 1fr))`,
          }}
        >
          <div className="bg-[#F7F8FA] p-2 dark:bg-muted" />
          {surfaces
            .filter((surface) => state.surfaces.includes(surface.id))
            .map((surface) => (
              <div
                key={surface.id}
                className="bg-[#F7F8FA] p-2 text-center font-mono text-[9px] tracking-wide uppercase dark:bg-muted"
              >
                {surface.name}
              </div>
            ))}
          {wanted.map((capabilityId) => {
            const capability = capabilities.find(
              (item) => item.id === capabilityId,
            );
            return (
              <div key={capabilityId} className="contents">
                <div className="bg-[#F7F8FA] p-2 font-mono text-[9px] tracking-wide uppercase dark:bg-muted">
                  {capability?.name}
                </div>
                {surfaces
                  .filter((surface) => state.surfaces.includes(surface.id))
                  .map((surface) => {
                    const covered =
                      recommended?.surfaces.includes(surface.id) === true &&
                      recommended.capabilities.includes(capabilityId);
                    return (
                      <div
                        key={surface.id}
                        className={cn(
                          "flex min-h-14 items-center justify-center bg-background",
                          covered && "bg-[#2873D7]",
                        )}
                      >
                        <span className="sr-only">
                          {covered ? "Projected coverage" : "Not projected"}
                        </span>
                        <span
                          aria-hidden="true"
                          className={cn(
                            "size-2 border",
                            covered
                              ? "border-white bg-white"
                              : "border-foreground/20",
                          )}
                        />
                      </div>
                    );
                  })}
              </div>
            );
          })}
        </div>
      </div>
      <div className="mt-5 flex items-center gap-2 font-mono text-[9px] tracking-[0.08em] uppercase">
        <span className="size-2 bg-[#2873D7]" /> Projected by recommendation
      </div>
    </div>
  );
}

function CoverageTable({
  state,
  evidence,
  telemetryStatus,
}: {
  state: PlannerState;
  evidence: Map<SurfaceId, SurfaceEvidence>;
  telemetryStatus: EvidenceStatus;
}) {
  const wanted = new Set(targetCapabilities(state.outcomes));
  return (
    <div className="border-border overflow-x-auto border">
      <table className="w-full min-w-[760px] border-collapse text-sm">
        <thead>
          <tr className="bg-muted/50">
            <th className="border-border border-b p-3 text-left font-medium">
              Capability / surface
            </th>
            {surfaces
              .filter((s) => state.surfaces.includes(s.id))
              .map((surface) => (
                <th
                  key={surface.id}
                  className="border-border border-b p-3 text-left font-medium"
                >
                  {surface.name}
                  <span className="text-muted-foreground block text-xs font-normal">
                    {surface.detail}
                  </span>
                </th>
              ))}
          </tr>
        </thead>
        <tbody>
          {capabilities.map((capability) => (
            <tr
              key={capability.id}
              className={cn(!wanted.has(capability.id) && "opacity-55")}
            >
              <th className="border-border border-b p-3 text-left font-normal">
                <span className="font-medium">{capability.name}</span>
                <span className="text-muted-foreground block text-xs">
                  {capability.description}
                </span>
              </th>
              {surfaces
                .filter((s) => state.surfaces.includes(s.id))
                .map((surface) => (
                  <td key={surface.id} className="border-border border-b p-3">
                    <EvidenceCell
                      capability={capability.id}
                      evidence={evidence.get(surface.id)}
                      telemetryStatus={telemetryStatus}
                    />
                  </td>
                ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function EvidenceCell({
  capability,
  evidence,
  telemetryStatus,
}: {
  capability: CapabilityId;
  evidence?: SurfaceEvidence;
  telemetryStatus: EvidenceStatus;
}) {
  const unsupportedLabels: Partial<Record<CapabilityId, string>> = {
    blocking: "Policy decision data unavailable",
    identity: "Attribution count unavailable",
    shadow: "Per-surface device data unavailable",
  };
  const unsupportedLabel = unsupportedLabels[capability];
  const value =
    capability === "session"
      ? (evidence?.sessions ?? 0)
      : capability === "cost"
        ? (evidence?.tokens ?? 0)
        : null;
  const label = unsupportedLabel
    ? unsupportedLabel
    : telemetryStatus === "loading"
      ? "Loading…"
      : telemetryStatus === "unavailable"
        ? "Evidence unavailable"
        : capability === "session"
          ? value
            ? `${value.toLocaleString()} sessions`
            : "No activity evidence"
          : value
            ? `${value.toLocaleString()} tokens`
            : "No token evidence";
  const observed = value !== null && value > 0;

  return (
    <div>
      <span
        className={cn(
          "inline-block size-2 rounded-full",
          observed ? "bg-success-default" : "bg-muted-foreground/30",
        )}
      />
      <span className="ml-2">{label}</span>
      {evidence?.lastSeen && observed && (
        <span className="text-muted-foreground mt-1 block text-xs">
          Last seen {evidence.lastSeen.toLocaleDateString()}
        </span>
      )}
    </div>
  );
}

function Metric({ value, label }: { value: string; label: string }) {
  return (
    <div>
      <p className="text-2xl font-semibold tracking-tight">{value}</p>
      <p className="text-muted-foreground mt-1 text-xs uppercase tracking-wide">
        {label}
      </p>
    </div>
  );
}

function buildEvidence(
  data: QueryResult | undefined,
): Map<SurfaceId, SurfaceEvidence> {
  const result = new Map<SurfaceId, SurfaceEvidence>();
  for (const row of data?.table ?? []) {
    if (
      !row.groupValue ||
      row.groupValue === "Other" ||
      /^Other \(\d+\)$/.test(row.groupValue)
    )
      continue;
    const surface = surfaceForHookSource(row.groupValue);
    const current = result.get(surface) ?? {
      sessions: 0,
      tokens: 0,
      lastSeen: null,
    };
    current.sessions += Number(row.measures.totalChats);
    current.tokens += Number(row.measures.totalTokens);
    const series = data?.timeseries.find(
      (item) => item.groupValue === row.groupValue,
    );
    const latest = [...(series?.points ?? [])]
      .reverse()
      .find(
        (point) =>
          Number(point.measures.totalChats) +
            Number(point.measures.totalToolCalls) +
            Number(point.measures.totalTokens) >
          0,
      );
    if (latest) {
      const latestDate = new Date(
        Number(BigInt(latest.bucketTimeUnixNano) / 1_000_000n),
      );
      if (!current.lastSeen || latestDate > current.lastSeen)
        current.lastSeen = latestDate;
    }
    result.set(surface, current);
  }
  return result;
}

function readState(key: string): PlannerState {
  try {
    const parsed = JSON.parse(
      sessionStorage.getItem(key) ?? "null",
    ) as Partial<PlannerState> | null;
    if (
      parsed &&
      Array.isArray(parsed.outcomes) &&
      Array.isArray(parsed.surfaces) &&
      parsed.answers &&
      [1, 2, 3, 4].includes(parsed.step ?? 0)
    )
      return parsed as PlannerState;
  } catch {
    /* Start from defaults. */
  }
  return defaultPlannerState;
}
