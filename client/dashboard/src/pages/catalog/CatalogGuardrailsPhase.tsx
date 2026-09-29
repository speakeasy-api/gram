import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import {
  ruleCategoryMeta,
  type RuleCategory,
} from "@/pages/security/policy-data";
import { SeverityBadge } from "@/pages/security/risk-ui";
import { ServerGuardrailsForm } from "@/pages/security/server-guardrails/ServerGuardrailsForm";
import {
  effectiveAction,
  hasFlagOnlyCategory,
  validateServerGuardrail,
} from "@/pages/security/server-guardrails/server-guardrail-policy";
import { useDetectorMode } from "@/pages/security/use-detector-mode";
import { ServerIcon } from "lucide-react";
import { useState, type ReactNode } from "react";
import { catalogLogoClassName } from "./logo";
import type {
  GuardrailServerSummary,
  GuardrailsPhase,
} from "./useRemoteMcpInstallWorkflow";

const ACTION_LABELS = {
  flag: { chip: "Log", label: "Log for review" },
  warn: { chip: "Warn", label: "Warn & confirm" },
  block: { chip: "Deny", label: "Deny the request" },
  quarantine: { chip: "Quarantine", label: "Quarantine session" },
} as const;

// Chip labels for the summary; the full detector names are too long to sit
// side by side.
const DETECTOR_CHIP_LABELS: Partial<Record<RuleCategory, string>> = {
  secrets: "Secrets",
  pii: "PII",
  destructive_tool: "Destructive tools",
  prompt_injection: "Prompt injection",
};

/** The catalog install's skippable Guardrails step: a recommended, pre-filled
 *  guardrail the user can switch off, summarized, with the full form one
 *  click away. */
export function CatalogGuardrailsPhase({
  releaseState,
  onClose,
}: {
  releaseState: GuardrailsPhase;
  onClose: () => void;
}): JSX.Element {
  const mode = useDetectorMode();
  const [enabled, setEnabled] = useState(true);
  const [customizing, setCustomizing] = useState(false);
  const { guardrail, servers } = releaseState;
  const single = servers.length === 1 ? servers[0] : undefined;
  const scopeName = single?.name ?? `${servers.length} servers`;
  const valid = validateServerGuardrail(guardrail).ok;

  return (
    <div className="space-y-4">
      <div className="space-y-2">
        {servers.map((server) => (
          <ServerSummaryRow key={server.key} server={server} />
        ))}
      </div>

      <div
        className={cn(
          "flex items-center justify-between gap-4 border p-3",
          enabled ? "border-foreground" : "border-border",
        )}
      >
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-sm font-medium">
              Create a risk policy for{" "}
              {single ? "this server" : "these servers"}
            </span>
            <Badge variant="success">Recommended</Badge>
          </div>
          <Text small muted>
            {single
              ? `Scoped to ${single.name} and its tools only.`
              : `Scoped to these ${servers.length} servers and their tools only.`}
          </Text>
        </div>
        <Switch
          aria-label="Create a risk policy"
          checked={enabled}
          onCheckedChange={setEnabled}
        />
      </div>

      {enabled ? (
        <>
          <GuardrailSummary
            releaseState={releaseState}
            single={single}
            mode={mode}
          />
          <Text small muted>
            Adjust detectors, tools or action later on the server&apos;s{" "}
            <span className="text-foreground font-medium">Guardrails</span> tab,
            or{" "}
            <button
              type="button"
              className="text-foreground font-medium underline underline-offset-2"
              onClick={() => setCustomizing((open) => !open)}
            >
              {customizing ? "hide options" : "customize now"}
            </button>
            .
          </Text>
          {customizing ? (
            <div className="max-h-[40vh] overflow-y-auto pr-1">
              <ServerGuardrailsForm
                state={guardrail}
                onChange={releaseState.updateGuardrail}
                toolsSource={{ status: "unavailable" }}
                serverName={scopeName}
                mode={mode}
              />
            </div>
          ) : null}
        </>
      ) : null}

      <Dialog.Footer>
        <div className="flex gap-2">
          {releaseState.goBack ? (
            <Button variant="tertiary" onClick={releaseState.goBack}>
              Back
            </Button>
          ) : null}
          <Button variant="tertiary" onClick={onClose}>
            Cancel
          </Button>
        </div>
        <div className="flex gap-2">
          <Button variant="secondary" onClick={() => void releaseState.skip()}>
            <Button.Text>Skip for now</Button.Text>
          </Button>
          <Button
            disabled={enabled && !valid}
            onClick={() =>
              void (enabled
                ? releaseState.installWithGuardrail()
                : releaseState.skip())
            }
          >
            <Button.Text>Add to Project</Button.Text>
          </Button>
        </div>
      </Dialog.Footer>
    </div>
  );
}

function ServerSummaryRow({
  server,
}: {
  server: GuardrailServerSummary;
}): JSX.Element {
  const facts = [
    `${server.toolCount} ${server.toolCount === 1 ? "tool" : "tools"}`,
    ...(server.destructiveTools.length > 0
      ? [`${server.destructiveTools.length} destructive`]
      : []),
    ...(server.oauth ? ["OAuth"] : []),
  ];
  return (
    <div className="flex items-center gap-3">
      <div className="bg-muted flex size-10 shrink-0 items-center justify-center border">
        {server.iconUrl ? (
          <img
            src={server.iconUrl}
            alt=""
            className={cn(
              "size-6",
              catalogLogoClassName(server.registrySpecifier),
            )}
          />
        ) : (
          <ServerIcon className="text-muted-foreground size-4" />
        )}
      </div>
      <div className="min-w-0">
        <div className="truncate text-sm font-medium">{server.name}</div>
        <Text small muted>
          {facts.join(" · ")}
        </Text>
      </div>
    </div>
  );
}

function GuardrailSummary({
  releaseState,
  single,
  mode,
}: {
  releaseState: GuardrailsPhase;
  single: GuardrailServerSummary | undefined;
  mode: ReturnType<typeof useDetectorMode>;
}): JSX.Element {
  const { guardrail, servers } = releaseState;
  const action = ACTION_LABELS[effectiveAction(guardrail)];
  const destructive = [
    ...new Set(servers.flatMap((server) => server.destructiveTools)),
  ];
  const inspectsDestructive = guardrail.categories.has("destructive_tool");
  const toolCount = single?.toolCount;

  return (
    <dl className="divide-border divide-y border px-3">
      <SummaryRow label="Detect">
        {guardrail.categories.size === 0 ? (
          <Text small muted>
            Nothing selected
          </Text>
        ) : (
          [...guardrail.categories].map((category) => (
            <Badge key={category} variant="neutral">
              {DETECTOR_CHIP_LABELS[category] ??
                ruleCategoryMeta(category, mode).label}
            </Badge>
          ))
        )}
      </SummaryRow>
      <SummaryRow label="Tools">
        <span className="text-sm">
          {toolCount ? `All ${toolCount} tools` : "All tools"}
          {inspectsDestructive && destructive.length > 0
            ? " · destructive:"
            : ""}
        </span>
        {inspectsDestructive
          ? destructive.map((tool) => (
              <Badge key={tool} variant="warning" className="normal-case">
                {tool}
              </Badge>
            ))
          : null}
      </SummaryRow>
      <SummaryRow label="Inspects">
        <span className="text-sm">Tool requests · Tool responses</span>
      </SummaryRow>
      <SummaryRow label="Action">
        <Badge variant={action.chip === "Log" ? "neutral" : "warning"}>
          {action.chip}
        </Badge>
        <span className="text-sm">{action.label} · Severity</span>
        <SeverityBadge score={guardrail.score} />
        <span className="text-sm">{guardrail.score.toFixed(1)}</span>
        {hasFlagOnlyCategory(guardrail.categories) ? (
          <Text small muted className="basis-full">
            Destructive tool detection supports logging only, so this policy
            logs.
          </Text>
        ) : null}
      </SummaryRow>
      <SummaryRow label="Audience">
        <span className="text-sm">
          {guardrail.audienceType === "everyone"
            ? "Everyone"
            : "Targeted users or roles"}
        </span>
      </SummaryRow>
    </dl>
  );
}

function SummaryRow({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="grid grid-cols-[6.5rem_1fr] items-start gap-3 py-2.5">
      <dt className="text-eyebrow pt-0.5">{label}</dt>
      <dd className="flex flex-wrap items-center gap-1.5">{children}</dd>
    </div>
  );
}
