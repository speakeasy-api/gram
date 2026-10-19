import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { Skeleton } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { useSdkClient } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import type { OpenAPIv3AffectedToolset } from "@gram/client/models/components/openapiv3affectedtoolset.js";
import type { OpenAPIv3Evaluation } from "@gram/client/models/components/openapiv3evaluation.js";
import type { OpenAPIv3EvaluationChange } from "@gram/client/models/components/openapiv3evaluationchange.js";
import { useQuery } from "@tanstack/react-query";
import { ChevronDownIcon } from "lucide-react";
import React from "react";
import { Link } from "react-router";
import { useStep } from "./step/use-step";
import type { ExistingDocument } from "./stepper/use-stepper";
import { useStepper } from "./stepper/use-stepper";

// A change lists at most this many detail lines before folding the rest
// behind a count, so one large change cannot push the deploy button far down.
const VISIBLE_DETAIL_LINES = 3;

const severityLabel: Record<OpenAPIv3EvaluationChange["severity"], string> = {
  breaking: "Breaking",
  warning: "Check",
  info: "Info",
};

const severityVariant = {
  breaking: "destructive",
  warning: "warning",
  info: "neutral",
} as const;

function plural(count: number, one: string, many = `${one}s`): string {
  return `${count} ${count === 1 ? one : many}`;
}

/** One line that says what the new version does to existing servers. */
function summarize(evaluation: OpenAPIv3Evaluation): string {
  const { breakingCount, warningCount, infoCount } = evaluation;
  if (breakingCount > 0) {
    return `${plural(breakingCount, "breaking change")} for servers that use this source.`;
  }
  if (warningCount > 0) {
    return `Nothing breaks, but ${plural(warningCount, "change")} ${warningCount === 1 ? "needs" : "need"} a look.`;
  }
  if (infoCount > 0) {
    return `Nothing breaks. ${plural(infoCount, "tool or description change")} only.`;
  }
  return "No changes to tools, inputs or credentials.";
}

/** Renders `backticked` names from change text as inline code. */
function ChangeText({ text }: { text: string }): React.JSX.Element {
  const parts = text.split(/`([^`]+)`/);
  return (
    <>
      {parts.map((part, i) =>
        i % 2 === 1 ? (
          <code key={i} className="font-mono text-[0.9em]">
            {part}
          </code>
        ) : (
          part
        ),
      )}
    </>
  );
}

function ChangeRow({
  change,
}: {
  change: OpenAPIv3EvaluationChange;
}): React.JSX.Element {
  const visible = change.details.slice(0, VISIBLE_DETAIL_LINES);
  const hidden = change.details.length - visible.length;

  const title = (
    <Text
      className={
        change.tools.length > 0
          ? "w-fit break-words underline decoration-dotted underline-offset-4"
          : "break-words"
      }
    >
      <ChangeText text={change.title} />
    </Text>
  );

  return (
    <li className="flex items-start gap-3 py-3">
      <Badge
        variant={severityVariant[change.severity]}
        size="sm"
        className="mt-0.5 w-20 shrink-0 justify-center text-center"
      >
        {severityLabel[change.severity]}
      </Badge>
      <div className="min-w-0 flex-1">
        {change.tools.length > 0 ? (
          <SimpleTooltip tooltip={change.tools.join(", ")}>
            {title}
          </SimpleTooltip>
        ) : (
          title
        )}
        {visible.map((line) => (
          <Text key={line} small muted className="block break-words">
            <ChangeText text={line} />
          </Text>
        ))}
        {hidden > 0 && (
          <SimpleTooltip
            tooltip={change.details.slice(VISIBLE_DETAIL_LINES).join("\n")}
          >
            <Text small muted className="block w-fit underline">
              {plural(hidden, "more line")}
            </Text>
          </SimpleTooltip>
        )}
      </div>
    </li>
  );
}

function AffectedServers({
  toolsets,
}: {
  toolsets: OpenAPIv3AffectedToolset[];
}): React.JSX.Element | null {
  const routes = useRoutes();
  const affected = toolsets.filter((t) => t.affectedToolCount > 0);
  if (affected.length === 0) return null;

  return (
    <Stack gap={1}>
      <span className="text-eyebrow">Affected MCP servers</span>
      <ul className="divide-y border-y">
        {affected.map((toolset) => (
          <li
            key={toolset.id}
            className="flex items-center justify-between gap-3 py-2"
          >
            <Link
              to={routes.mcp.details.href(toolset.slug)}
              className="min-w-0 truncate underline-offset-2 hover:underline"
              title={toolset.name}
            >
              {toolset.name}
            </Link>
            <Text small muted className="shrink-0 whitespace-nowrap">
              {toolset.affectedToolCount} of {plural(toolset.toolCount, "tool")}{" "}
              affected
            </Text>
          </li>
        ))}
      </ul>
    </Stack>
  );
}

function EvaluationResult({
  evaluation,
}: {
  evaluation: OpenAPIv3Evaluation;
}): React.JSX.Element {
  const needsAttention = evaluation.changes.filter(
    (c) => c.severity !== "info",
  );
  const informational = evaluation.changes.filter((c) => c.severity === "info");

  return (
    <Stack gap={4}>
      <Text
        className={
          evaluation.breakingCount > 0 ? "text-destructive" : undefined
        }
      >
        {summarize(evaluation)}
      </Text>

      {needsAttention.length > 0 && (
        <ul className="divide-y border-y">
          {needsAttention.map((change, i) => (
            <ChangeRow key={`${change.code}-${i}`} change={change} />
          ))}
        </ul>
      )}

      <AffectedServers toolsets={evaluation.affectedToolsets} />

      {informational.length > 0 && (
        <Collapsible>
          <CollapsibleTrigger className="group flex items-center gap-1">
            <Text small muted>
              {plural(informational.length, "other change")}
            </Text>
            <ChevronDownIcon className="text-muted-foreground size-4 transition-transform group-data-[state=open]:rotate-180" />
          </CollapsibleTrigger>
          <CollapsibleContent>
            <ul className="divide-y border-b">
              {informational.map((change, i) => (
                <ChangeRow key={`${change.code}-${i}`} change={change} />
              ))}
            </ul>
          </CollapsibleContent>
        </Collapsible>
      )}
    </Stack>
  );
}

/**
 * The second step when the upload replaces a document already in the
 * project: compares the uploaded file with the deployed version and shows
 * what it changes for the servers that use it before anything is deployed.
 */
export function ReviewChangesStep({
  existingDocument,
  assetId,
  onContinue,
}: {
  existingDocument: ExistingDocument;
  assetId: string;
  onContinue: () => void;
}): React.JSX.Element {
  const client = useSdkClient();
  const evaluation = useQuery({
    queryKey: ["evaluateOpenAPIv3", assetId, existingDocument.slug],
    queryFn: () =>
      client.deployments.evaluateOpenAPIv3({
        evaluateOpenAPIv3Form: {
          assetId,
          slug: existingDocument.slug,
          name: existingDocument.name,
        },
      }),
    // The baseline is the active deployment, which can change while the
    // review is open, so the comparison is not cached across visits. It runs
    // two full extractions on the server, so it does not rerun on focus.
    staleTime: 0,
    refetchOnWindowFocus: false,
    retry: false,
  });

  const hasBreaking = (evaluation.data?.breakingCount ?? 0) > 0;

  return (
    <Stack gap={4} className="max-w-2xl">
      {evaluation.isPending && (
        <Stack gap={2}>
          <Text muted>Comparing with the deployed version…</Text>
          <Skeleton>
            <div className="h-5 w-80" />
            <div className="h-5 w-full" />
            <div className="h-5 w-3/4" />
          </Skeleton>
        </Stack>
      )}

      {evaluation.isError && (
        <Alert variant="warning" dismissible={false}>
          The new version could not be compared with the deployed one, so its
          changes are unknown. You can try again or deploy it without a review.
        </Alert>
      )}

      {evaluation.data && <EvaluationResult evaluation={evaluation.data} />}

      <Stack direction="horizontal" gap={2} className="items-center">
        <Button
          variant="brand"
          disabled={evaluation.isPending}
          onClick={onContinue}
        >
          {hasBreaking ? "DEPLOY ANYWAY" : "DEPLOY NEW VERSION"}
        </Button>
        {evaluation.isError && (
          <Button variant="secondary" onClick={() => void evaluation.refetch()}>
            Try again
          </Button>
        )}
      </Stack>
    </Stack>
  );
}

/** Wires the review into the upload stepper. */
export default function ReviewChangesStepContainer(): React.JSX.Element | null {
  const stepper = useStepper();
  const step = useStep();
  const { existingDocument, uploadResult } = stepper.meta.current;

  if (!existingDocument) return null;

  if (step.isCurrentStep && step.state === "idle" && uploadResult) {
    return (
      <ReviewChangesStep
        existingDocument={existingDocument}
        assetId={uploadResult.asset.id}
        onContinue={() => {
          stepper.meta.current.assetName = existingDocument.name;
          step.setState("completed");
          stepper.next();
        }}
      />
    );
  }

  if (step.state === "completed") {
    return <Text>✓ New version of {existingDocument.name} reviewed</Text>;
  }

  return null;
}
