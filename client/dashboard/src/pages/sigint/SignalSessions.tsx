import { InlineEmptyState } from "@/components/inline-empty-state";
import { Page } from "@/components/page-layout";
import { StatRow } from "@/components/stat-row";
import { Button } from "@/components/ui/Button";
import {
  Select,
  SelectTrigger,
  SelectContent,
  SelectValue,
  SelectItem,
} from "@/components/ui/Select";
import { Skeleton } from "@/components/ui/Skeleton";
import { Table } from "@/components/ui/Table";
import { useProject } from "@/contexts/Auth";
import { downloadBlob } from "@/lib/download";
import { agentSessionHref } from "@/pages/chatLogs/agentSessionLink";
import { useRoutes } from "@/routes";
import { analyticsSignalFindings } from "@gram/client/funcs/analyticsSignalFindings.js";
import type { AnalyticsQueryPayload } from "@gram/client/models/components/analyticsquerypayload.js";
import type { AnalyticsSignalCatalog } from "@gram/client/models/components/analyticssignalcatalog.js";
import type { AnalyticsSignalFindings } from "@gram/client/models/components/analyticssignalfindings.js";
import type { SignalFindingsRequestBody } from "@gram/client/models/components/signalfindingsrequestbody.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { unwrapAsync } from "@gram/client/types/fp.js";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";
import { signalNumber } from "./explorationModel";

export function SignalSessions({
  request,
  catalog,
}: {
  request: AnalyticsQueryPayload;
  catalog: AnalyticsSignalCatalog;
}): JSX.Element {
  const [peerId, setPeerId] = useState("none");
  const [peerSignal, setPeerSignal] = useState("");
  const [submitted, setSubmitted] = useState<SignalFindingsRequestBody | null>(
    null,
  );
  const project = useProject();
  const client = useGramContext();
  const peer = catalog.sensors.find((sensor) => sensor.id === peerId);
  const selected = peer?.signalIds.includes(peerSignal)
    ? peerSignal
    : peer?.signalIds[0];
  const names = new Map(
    catalog.signals.map((signal) => [signal.id, signal.name]),
  );
  const body: SignalFindingsRequestBody = {
    from: request.from,
    to: request.to,
    signalQuery: { ...request.signalQuery!, groupBy: [] },
    comparePrevious: true,
    evidence: true,
  };
  if (peer && selected) {
    body.peer = {
      sensorId: peer.id,
      threshold: request.signalQuery!.selection.threshold,
    };
    if (peer.mode === "ordered_score")
      body.peer.minimumLevel = peer.signalIds.indexOf(selected);
    else body.peer.signalId = selected;
  }
  const query = useQuery({
    queryKey: ["signal-session-findings", project.id, submitted],
    enabled: submitted !== null,
    retry: false,
    throwOnError: false,
    refetchOnWindowFocus: false,
    queryFn: ({ signal }) =>
      unwrapAsync(
        analyticsSignalFindings(
          client,
          { signalFindingsRequestBody: submitted! },
          undefined,
          { fetchOptions: { signal } },
        ),
      ),
  });
  return (
    <section className="space-y-4 border-t pt-5">
      <div>
        <h2 className="text-display-xs">Across sessions</h2>
        <p className="text-muted-foreground text-sm">
          Check how widely this signal appears, what it appears alongside, and
          which conversations support it.
        </p>
      </div>
      <Page.Toolbar>
        <Page.Toolbar.Leading>
          <Select value={peerId} onValueChange={setPeerId}>
            <SelectTrigger
              className="max-w-64"
              aria-label="Co-occurrence sensor"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="none">No second signal</SelectItem>
              {catalog.sensors.map((sensor) => (
                <SelectItem key={sensor.id} value={sensor.id}>
                  {sensor.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {peer && (
            <Select value={selected} onValueChange={setPeerSignal}>
              <SelectTrigger
                className="max-w-64"
                aria-label="Co-occurring signal"
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {peer.signalIds.map((id) => (
                  <SelectItem
                    key={id}
                    value={id}
                  >{`${peer.mode === "ordered_score" ? "At least " : ""}${names.get(id) ?? "Removed signal"}`}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        </Page.Toolbar.Leading>
        <Page.Toolbar.Actions>
          <Button
            variant="secondary"
            disabled={query.isFetching}
            onClick={() => {
              if (JSON.stringify(submitted) === JSON.stringify(body))
                void query.refetch();
              else setSubmitted({ ...body });
            }}
          >
            Analyze sessions
          </Button>
        </Page.Toolbar.Actions>
      </Page.Toolbar>
      {submitted && query.isPending && <Skeleton className="h-64 w-full" />}
      {query.isError && (
        <InlineEmptyState
          icon="triangle-alert"
          heading="Session analysis couldn’t be completed"
          description="Try a shorter chart window, or retry the analysis."
          action={
            <Button variant="secondary" onClick={() => void query.refetch()}>
              Try again
            </Button>
          }
        />
      )}
      {query.data && !query.isError && (
        <SessionFindings
          result={query.data}
          request={submitted!}
          catalog={catalog}
        />
      )}
    </section>
  );
}

function SessionFindings({
  result,
  request,
  catalog,
}: {
  result: AnalyticsSignalFindings;
  request: SignalFindingsRequestBody;
  catalog: AnalyticsSignalCatalog;
}): JSX.Element {
  const [showAll, setShowAll] = useState(false);
  const routes = useRoutes();
  const summary = result.measurement.summary;
  const comparison = result.comparison;
  const measured = result.measurement.metadata;
  const peerSensor = catalog.sensors.find(
    (sensor) => sensor.id === result.peer?.sensorId,
  );
  const primarySensor = catalog.sensors.find(
    (sensor) => sensor.id === measured.selection.sensorId,
  );
  const peerRule =
    peerSensor?.mode === "exclusive"
      ? "selected answer"
      : `minimum probability ${result.peer?.threshold}`;
  const dates: Intl.DateTimeFormatOptions = {
    month: "short",
    day: "numeric",
    year: "numeric",
    timeZone: "UTC",
  };
  const peerSignal =
    result.peer?.signalId ??
    peerSensor?.signalIds[result.peer?.minimumLevel ?? 0];
  const peerName =
    catalog.signals.find((signal) => signal.id === peerSignal)?.name ??
    "Selected signal";
  return (
    <div className="space-y-4">
      <p className="text-muted-foreground text-sm">
        {measured.from.toLocaleDateString("en-US", dates)} –{" "}
        {measured.to.toLocaleDateString("en-US", dates)} UTC · filtered activity
        · original captured messages
      </p>
      <StatRow
        className="flex-col divide-x-0 divide-y sm:flex-row sm:divide-x sm:divide-y-0"
        metrics={[
          {
            label: "Share of evaluated sessions",
            value: signalNumber(summary.session_breadth, true),
            tone: "information",
            description: `${signalNumber(summary.matched_sessions)} of ${signalNumber(summary.evaluated_sessions)} evaluated conversations`,
          },
          {
            label: "Top-session concentration",
            value: signalNumber(result.concentration.top_session_share, true),
            tone: "information",
            description: "Share of linked matches in one conversation",
          },
          {
            label: "Top five sessions",
            value: signalNumber(
              result.concentration.top_five_session_share,
              true,
            ),
            tone: "information",
            description: `${signalNumber(result.concentration.linked_matched_messages)} total linked matches`,
          },
        ]}
      />
      {result.cooccurrence.length > 0 && (
        <div className="space-y-2">
          <p className="text-eyebrow">Where both signals appear</p>
          <p className="text-sm">
            With {result.peer?.minimumLevel !== undefined && "at least "}
            {peerName} · {peerSensor?.name ?? "Selected sensor"} · {peerRule}
          </p>
          <Table
            data={result.cooccurrence}
            rowKey={(row) => String(row.unit)}
            columns={[
              {
                key: "unit",
                header: "Counted together",
                render: (row) =>
                  row.unit === "same_message" ? "Same message" : "Same session",
              },
              {
                key: "joint_matches",
                header: "Both signals",
                render: (row) => signalNumber(row.joint_matches),
              },
              {
                key: "joint_answered",
                header: "Both measured",
                render: (row) => signalNumber(row.joint_answered),
              },
              {
                key: "joint_share",
                header: "Joint share",
                render: (row) => signalNumber(row.joint_share, true),
              },
            ]}
          />
          <p className="text-muted-foreground text-sm">
            Same-session matches may occur in different messages. Each
            denominator contains only observations measured by both predicates.
          </p>
        </div>
      )}
      {comparison && (
        <div className="space-y-2 border-t pt-4">
          <p className="text-eyebrow">Previous equal-duration window</p>
          <p>
            {signalNumber(comparison.summary.match_share, true)} →{" "}
            {signalNumber(summary.match_share, true)} share of measured answers
          </p>
          <p className="text-muted-foreground text-sm">
            {comparison.comparable
              ? "Observed definitions and sample sizes are comparable."
              : "Interpret this change cautiously: " +
                [...new Set(comparison.reasons.map(comparisonReason))].join(
                  "; ",
                ) +
                "."}{" "}
            Coverage of all eligible messages is unknown. This is a descriptive
            comparison, not a significance test.
          </p>
        </div>
      )}
      <div className="space-y-2 border-t pt-4">
        <p className="text-eyebrow">Supporting conversations</p>
        <p className="text-muted-foreground text-sm">
          This sample includes only conversations you can access.{" "}
          {result.peer &&
            "Both selected signals occur in each example conversation."}
        </p>
        {result.evidence.length === 0 && (
          <p className="text-sm">
            No permitted examples in this bounded sample.
          </p>
        )}
        {result.evidence.length > 0 && (
          <Table
            data={showAll ? result.evidence : result.evidence.slice(0, 5)}
            rowKey={(row) => String(row.reading_id)}
            columns={[
              {
                key: "time",
                header: "Matching activity",
                render: (row) =>
                  new Date(String(row.event_occurred_at)).toLocaleString(
                    "en-US",
                    { ...dates, hour: "2-digit", minute: "2-digit" },
                  ) + " UTC",
              },
              {
                key: "probability",
                header: "Matching evidence",
                render: (row) =>
                  primarySensor?.mode === "exclusive"
                    ? "Selected answer"
                    : signalNumber(row.rule_value, true),
              },
              {
                key: "open",
                header: "Evidence",
                render: (row) => (
                  <Link
                    className="underline"
                    to={agentSessionHref(
                      routes.agentSessions.href(),
                      String(row.conversation_id),
                    )}
                  >
                    Open conversation
                  </Link>
                ),
              },
            ]}
          />
        )}
        {result.evidence.length > 5 && (
          <Button
            variant="tertiary"
            size="sm"
            onClick={() => setShowAll(!showAll)}
          >
            {showAll
              ? "Show fewer examples"
              : `Show all ${result.evidence.length} examples`}
          </Button>
        )}
      </div>
      <Button
        variant="secondary"
        size="sm"
        onClick={() =>
          downloadBlob(
            new Blob([JSON.stringify({ request, result }, null, 2)], {
              type: "application/json",
            }),
            "signal-session-findings.json",
          )
        }
      >
        Export findings
      </Button>
    </div>
  );
}

function comparisonReason(reason: string): string {
  if (reason === "insufficient_observations")
    return "fewer than 30 answers or five evaluated sessions in a window";
  if (reason === "recent_window_unsettled")
    return "the current window ends within the last five minutes and may still receive readings";
  if (reason === "definition_changed_mixed_or_unknown")
    return "measurement definitions changed, are mixed, or are unknown";
  return "classifier or input settings changed, are mixed, or are unknown";
}
