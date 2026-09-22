import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { telemetryQuery } from "@gram/client/funcs/telemetryQuery";
import type { QueryResult } from "@gram/client/models/components/queryresult.js";
import { useDeviceIntegrationCoverage } from "@gram/client/react-query/deviceIntegrationCoverage.js";
import { unwrapAsync } from "@gram/client/types/fp.js";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/Button";
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
        <Page.Section>
          <Page.Section.Title area="Platform Admin">
            Support coverage planner
          </Page.Section.Title>
          <Page.Section.Description>
            Assess the current organization, recommend the next integration, and
            compare projected coverage with evidence received in the last 30
            days.
          </Page.Section.Description>
          <Page.Section.Body>
            <PlatformAdminGate>
              <Planner />
            </PlatformAdminGate>
          </Page.Section.Body>
        </Page.Section>
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
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b pb-4">
        <div className="flex items-center gap-2">
          <InternalAdminBadge />
          <Badge variant="neutral">
            <Badge.Text>{organization.name}</Badge.Text>
          </Badge>
        </div>
        <div className="text-muted-foreground text-xs">
          Session-only plan · actual evidence · {WINDOW_DAYS}-day window
        </div>
      </div>
      <StepRail step={state.step} onStep={(step) => update({ step })} />

      {state.step === 1 && (
        <ChoiceStep
          eyebrow="Step 01 / 04"
          title="What does this organization want to achieve?"
          description="These outcomes rank recommendations; they do not hide possible integrations."
          items={outcomes.map((item) => ({
            ...item,
            selected: state.outcomes.includes(item.id),
            onClick: () =>
              update({ outcomes: toggle(state.outcomes, item.id) }),
          }))}
          onNext={() => update({ step: 2 })}
        />
      )}
      {state.step === 2 && (
        <ChoiceStep
          eyebrow="Step 02 / 04"
          title="Which surfaces are in scope?"
          description="Coverage is evaluated per surface using the organization’s observed activity."
          items={surfaces.map((item) => ({
            id: item.id,
            name: item.name,
            description: item.detail,
            selected: state.surfaces.includes(item.id),
            onClick: () =>
              update({ surfaces: toggle(state.surfaces, item.id) }),
          }))}
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
    <div className="grid grid-cols-4 gap-1">
      {[1, 2, 3, 4].map((value) => (
        <button
          key={value}
          type="button"
          onClick={() => onStep(value as PlannerState["step"])}
          disabled={value > step}
          className={cn(
            "h-1.5 disabled:cursor-default",
            value <= step ? "bg-foreground" : "bg-muted",
          )}
          aria-label={`Go to step ${value}`}
          aria-current={value === step ? "step" : undefined}
        />
      ))}
    </div>
  );
}

type ChoiceItem = {
  id: string;
  name: string;
  description: string;
  selected: boolean;
  onClick: () => void;
};
function ChoiceStep({
  eyebrow,
  title,
  description,
  items,
  onBack,
  onNext,
}: {
  eyebrow: string;
  title: string;
  description: string;
  items: ChoiceItem[];
  onBack?: () => void;
  onNext: () => void;
}) {
  return (
    <div className="mx-auto max-w-5xl space-y-7 py-6">
      <div>
        <p className="text-eyebrow">{eyebrow}</p>
        <h2 className="mt-2 text-3xl font-semibold tracking-tight">{title}</h2>
        <p className="text-muted-foreground mt-2 max-w-2xl">{description}</p>
      </div>
      <div className="grid gap-3 md:grid-cols-2 lg:grid-cols-3">
        {items.map((item) => (
          <button
            key={item.id}
            type="button"
            onClick={item.onClick}
            aria-pressed={item.selected}
            className={cn(
              "bg-card min-h-28 border p-4 text-left transition-colors",
              item.selected
                ? "border-foreground ring-1 ring-foreground"
                : "border-border hover:border-foreground/50",
            )}
          >
            <div className="flex items-start justify-between gap-3">
              <span className="font-medium">{item.name}</span>
              <span
                className={cn(
                  "mt-0.5 size-3 border",
                  item.selected && "bg-foreground",
                )}
              />
            </div>
            <p className="text-muted-foreground mt-2 text-sm">
              {item.description}
            </p>
          </button>
        ))}
      </div>
      <div className="flex gap-2">
        {onBack && (
          <Button variant="secondary" onClick={onBack}>
            Back
          </Button>
        )}
        <Button
          onClick={onNext}
          disabled={!items.some((item) => item.selected)}
        >
          Continue
        </Button>
      </div>
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
    <div className="mx-auto max-w-4xl space-y-7 py-6">
      <div>
        <p className="text-eyebrow">Step 03 / 04</p>
        <h2 className="mt-2 text-3xl font-semibold tracking-tight">
          A few details about the setup
        </h2>
        <p className="text-muted-foreground mt-2">
          These answers only affect recommendation eligibility and remain in
          this browser tab.
        </p>
      </div>
      <div className="divide-border border-border bg-card divide-y border">
        {questions.map((question) => (
          <div
            key={question.id}
            className="grid gap-4 p-5 md:grid-cols-[1fr_1.4fr] md:items-center"
          >
            <p id={`${question.id}-label`} className="font-medium">
              {question.title}
            </p>
            <div
              className="flex flex-wrap gap-2"
              role="group"
              aria-labelledby={`${question.id}-label`}
            >
              {question.options.map((option) => (
                <Button
                  key={option}
                  size="sm"
                  variant={
                    state.answers[question.id] === option
                      ? "primary"
                      : "secondary"
                  }
                  aria-pressed={state.answers[question.id] === option}
                  onClick={() =>
                    update({
                      answers: { ...state.answers, [question.id]: option },
                    })
                  }
                >
                  {option}
                </Button>
              ))}
            </div>
          </div>
        ))}
      </div>
      <div className="flex gap-2">
        <Button variant="secondary" onClick={() => update({ step: 2 })}>
          Back
        </Button>
        <Button onClick={() => update({ step: 4 })}>Build coverage plan</Button>
      </div>
    </div>
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
    <div className="space-y-8 py-3">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p className="text-eyebrow">
            Step 04 / 04 · Recommended first integration
          </p>
          <h2 className="mt-2 text-3xl font-semibold tracking-tight">
            {recommended?.name ?? "No eligible integration"}
          </h2>
          <p className="text-muted-foreground mt-2 max-w-2xl">
            {recommended?.description ??
              "Adjust the setup answers or selected surfaces to see a recommendation."}
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="secondary" onClick={onBack}>
            Back
          </Button>
          <Button variant="secondary" onClick={onReset}>
            Start over
          </Button>
        </div>
      </div>
      {recommended && (
        <div className="border-border bg-card grid gap-4 border p-5 md:grid-cols-3">
          <Metric
            value={`${recommendationCells}/${targetCells}`}
            label="target cells addressed"
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
                ? `device agents active within ${activeWindowMinutes} minutes`
                : "active device agents now"
            }
          />
        </div>
      )}
      <section className="space-y-3">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h3 className="text-xl font-semibold">Observed coverage</h3>
            <p className="text-muted-foreground text-sm">
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
      <section className="space-y-3">
        <h3 className="text-xl font-semibold">Integration paths</h3>
        <div className="grid gap-3 md:grid-cols-2">
          {methods
            .filter(
              (method) => !method.eligible || method.eligible(state.answers),
            )
            .map((method) => (
              <div
                key={method.id}
                className={cn(
                  "border-border bg-card border p-4",
                  method.id === recommended?.id && "border-foreground",
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
