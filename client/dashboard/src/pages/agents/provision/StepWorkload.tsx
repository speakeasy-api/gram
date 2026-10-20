import { isRelativePath } from "@/components/setup-steps/origin";
import { Badge } from "@/components/ui/Badge";
import { Boxes, Cloud, GitBranch, KeyRound, ShieldCheck } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { CUSTOM_PLATFORM_KEY } from "./CustomSetupInstructions";
import { hasPlatformSetup } from "./platformSetups";
import type { SetupStep } from "./setupInstructions";
import type { CatalogEntry } from "./workloadCatalog";

import type { JSX } from "react";
import { WizardStepHeader } from "./WizardChrome";
import type { WorkloadSetup } from "./useWorkloadSetup";

/** A platform's own logo where it has one, a plain glyph where it does not. */
function PlatformMark({ entry }: { entry: PickerEntry }): JSX.Element | null {
  if (entry.icon !== undefined && isRelativePath(entry.icon)) {
    return <img src={entry.icon} alt="" className="mt-0.5 size-6 shrink-0" />;
  }
  if (entry.Icon === undefined) return null;
  return (
    <entry.Icon
      aria-hidden="true"
      className="text-muted-foreground mt-0.5 size-6 shrink-0"
    />
  );
}

/** A tile in the picker: a catalog platform, the custom flow, or a plan. */
type PickerEntry = {
  key: string;
  displayName: string;
  description: string;
  /** A platform's own mark, when the catalog ships one. */
  icon?: string;
  /** A plain glyph, where there is no mark to show. */
  Icon?: LucideIcon;
};

const PLANNED: PickerEntry[] = [
  {
    key: "github-actions",
    displayName: "GitHub Actions",
    description: "Let a workflow call your MCP servers with its OIDC token.",
    Icon: GitBranch,
  },
  {
    key: "kubernetes",
    displayName: "Kubernetes",
    description: "Let a pod call your MCP servers with its service account.",
    Icon: Boxes,
  },
  {
    key: "spiffe",
    displayName: "SPIFFE",
    description: "Let a workload call your MCP servers with its SVID.",
    Icon: ShieldCheck,
  },
  {
    key: "gcp",
    displayName: "Google Cloud",
    description:
      "Let a service account call your MCP servers with its identity token.",
    Icon: Cloud,
  },
];

/**
 * The platforms this flow can federate a token from: the catalog entries the
 * server ships a guided setup for. An entry without one is left out rather
 * than listed and refused — there is nothing this wizard could do with it.
 */
export function WorkloadPlatformPicker({
  entries,
  isPending,
  isError,
  onRetry,
  selectedKey,
  disabled = false,
  onSelect,
}: {
  entries: CatalogEntry[];
  isPending: boolean;
  isError: boolean;
  onRetry: () => void;
  selectedKey: string | null;
  /** Settled: the agent already signs in with one of these. */
  disabled?: boolean;
  onSelect: (key: string) => void;
}): JSX.Element {
  // The catalog's policy is necessary but not sufficient: a platform is
  // offered only where this wizard also ships instructions for it.
  const available: PickerEntry[] = [
    ...entries
      .filter((entry) => entry.enabled && hasPlatformSetup(entry.key))
      .map((entry) => ({
        key: entry.key,
        displayName: entry.displayName,
        description: entry.description,
        icon: entry.icon,
      })),
    {
      key: CUSTOM_PLATFORM_KEY,
      displayName: "Anything else",
      description:
        "Federate any OIDC issuer by entering its URLs yourself — a CI provider, a cluster, your own identity service.",
      Icon: KeyRound,
    },
  ];

  if (isError) {
    return (
      <div role="alert" className="space-y-3">
        <Text>Could not load the platforms Speakeasy can federate from.</Text>
        <Button variant="secondary" onClick={onRetry}>
          Try again
        </Button>
      </div>
    );
  }
  if (isPending) return <Text muted>Loading platforms…</Text>;

  // The catalog ships one platform at a time, and the credential card names
  // four. Without these the reader is left to guess whether the rest are
  // unsupported or merely absent today.
  const soon: PickerEntry[] = [
    ...entries
      .filter((entry) => !hasPlatformSetup(entry.key))
      .map((entry) => ({
        key: entry.key,
        displayName: entry.displayName,
        description: entry.description,
        icon: entry.icon,
      })),
    ...PLANNED.filter(
      (planned) => !entries.some((entry) => entry.key === planned.key),
    ),
  ];

  if (available.length === 0 && soon.length === 0) {
    return (
      <Text muted small>
        No platform with a guided setup is available yet. Issue an API key
        instead.
      </Text>
    );
  }

  return (
    <div className="space-y-3">
      <p className="text-eyebrow">Platform</p>
      <ul className="grid gap-3 sm:grid-cols-2">
        {available.map((entry) => {
          const chosen = entry.key === selectedKey;
          return (
            <li key={entry.key}>
              <label
                className={cn(
                  "border-border flex h-full items-start gap-3 border p-3",
                  // The radio itself is hidden, so the tile has to carry the
                  // focus ring or keyboard focus lands nowhere visible.
                  "has-[input:focus-visible]:ring-ring/40 has-[input:focus-visible]:ring-1",
                  disabled ? "cursor-default" : "cursor-pointer",
                  chosen
                    ? "border-foreground bg-accent/40"
                    : !disabled && "hover:bg-muted/40",
                  disabled && !chosen && "opacity-60",
                )}
              >
                <input
                  type="radio"
                  name="workload-platform"
                  className="sr-only"
                  checked={chosen}
                  disabled={disabled}
                  onChange={() => onSelect(entry.key)}
                />
                <PlatformMark entry={entry} />
                <span className="min-w-0 flex-1">
                  <span className="block">{entry.displayName}</span>
                  <Text muted small className="block">
                    {entry.description}
                  </Text>
                </span>
              </label>
            </li>
          );
        })}
        {soon.map((entry) => (
          <li key={entry.key}>
            <div
              aria-disabled="true"
              className="border-border flex h-full items-start gap-3 border border-dashed p-3 opacity-60"
            >
              <PlatformMark entry={entry} />
              <span className="min-w-0 flex-1">
                <span className="flex items-center gap-2">
                  <span className="truncate">{entry.displayName}</span>
                  <Badge variant="neutral" size="sm">
                    Coming soon
                  </Badge>
                </span>
                <Text muted small className="block">
                  {entry.description}
                </Text>
              </span>
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}

/** One step of a platform's setup, as a step of this wizard. */
export function StepWorkload({
  step,
  setup,
  first,
  platformName,
}: {
  step: SetupStep;
  setup: WorkloadSetup;
  /** Whether this is the platform's first step, which carries what it needs. */
  first: boolean;
  platformName: string;
}): JSX.Element {
  return (
    <div className="space-y-6">
      <WizardStepHeader title={step.title} />
      {setup.error !== null && (
        <p
          role="alert"
          className="border-destructive text-destructive border p-3 text-sm"
        >
          {setup.error}
        </p>
      )}
      <div className="space-y-4">{setup.render(step)}</div>
      {/* A precondition, not an instruction: it sits under the work, folded
          away, because the reader who already has it must not be stopped by
          it on the way in. */}
      {first && setup.requirements !== null && (
        <details>
          <summary className="text-muted-foreground hover:text-foreground cursor-pointer text-sm">
            What you need for {platformName}
          </summary>
          <div className="bg-muted/40 border-muted-foreground/30 mt-2 border p-4">
            {setup.requirements}
          </div>
        </details>
      )}
    </div>
  );
}
