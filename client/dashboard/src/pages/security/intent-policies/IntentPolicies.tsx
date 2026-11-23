import { InlineEmptyState } from "@/components/inline-empty-state";
import { Page } from "@/components/page-layout";
import { Alert, AlertDescription } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Combobox } from "@/components/ui/Combobox";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import {
  PageTabsList,
  PageTabsTrigger,
  Tabs,
  TabsContent,
} from "@/components/ui/Tabs";
import { Text } from "@/components/ui/Text";
import { useIsPlatformAdmin } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { handleError } from "@/lib/errors";
import { cn } from "@/lib/utils";
import { Evaluator } from "@gram/client/models/components/analyzesessionrequestbody.js";
import type { IntentGuardrail } from "@gram/client/models/components/intentguardrail.js";
import type { IntentSessionAnalysis } from "@gram/client/models/components/intentsessionanalysis.js";
import { SortBy, SortOrder } from "@gram/client/models/operations/listchats";
import { useIntentGuardrail } from "@gram/client/react-query/intentGuardrail.js";
import { useListChats } from "@gram/client/react-query/listChats.js";
import { useUploadIntentTranscriptMutation } from "@gram/client/react-query/uploadIntentTranscript.js";
import { fetchFullTranscript } from "@/pages/chatLogs/transcriptFull";
import {
  keepPreviousData,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import {
  useDeferredValue,
  useMemo,
  useRef,
  useState,
  type DragEvent,
} from "react";
import { useSearchParams } from "react-router";
import { toast } from "sonner";
import { downloadBlob } from "@/lib/download";
import {
  ANALYSIS_BATCH,
  analyzableCalls,
  buildReport,
  mergeBatch,
  reportFilename,
  type AnalysisProgress,
} from "./analysisRun";
import { buildSessionEvents } from "./events";
import { GuardrailView } from "./GuardrailView";
import { callOutcome, formatCost, formatDuration } from "./model";
import { SessionViewer, type ReplayFocus } from "./SessionViewer";
import { HATCH_STYLE, OUTCOME_FILL } from "./styles";

const SESSION_LIMIT = 50;

// Mirrors the server's upload cap so oversized files fail before the request.
const MAX_UPLOAD_BYTES = 20 << 20;

const EVALUATOR_OPTIONS = [
  { value: Evaluator.Jev, label: "Jev" },
  { value: Evaluator.Gemini, label: "Gemini" },
];

export default function IntentPolicies(): JSX.Element {
  const isPlatformAdmin = useIsPlatformAdmin();
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title stage="preview">
            Intent-based policies
          </Page.Section.Title>
          <Page.Section.Description>
            Replay a session against the intent guardrail and see which tool
            calls it would permit, ask about, or block. Nothing is enforced.
          </Page.Section.Description>
          <Page.Section.Body>
            {isPlatformAdmin ? (
              <Workbench />
            ) : (
              <Text muted className="py-8 text-center">
                This page is available to platform admins only.
              </Text>
            )}
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}

function Workbench(): JSX.Element {
  const queryClient = useQueryClient();
  // Errors render inline (Retry) rather than taking down the page.
  const guardrailQuery = useIntentGuardrail(undefined, undefined, {
    throwOnError: false,
  });
  const [search, setSearch] = useState("");
  const deferredSearch = useDeferredValue(search.trim());
  const chatsQuery = useListChats(
    {
      search: deferredSearch || undefined,
      limit: SESSION_LIMIT,
      sortBy: SortBy.LastMessageTimestamp,
      sortOrder: SortOrder.Desc,
    },
    undefined,
    { placeholderData: keepPreviousData },
  );
  const [chatId, setChatId] = useState<string | undefined>();
  const [chatTitle, setChatTitle] = useState<string | undefined>();
  const [evaluator, setEvaluator] = useState<Evaluator>(Evaluator.Jev);
  const [analysis, setAnalysis] = useState<IntentSessionAnalysis | null>(null);
  const [focus, setFocus] = useState<ReplayFocus | null>(null);
  const [params, setParams] = useSearchParams();
  const tab = params.get("tab") === "guardrail" ? "guardrail" : "replay";
  const [dragging, setDragging] = useState(false);
  const fileInput = useRef<HTMLInputElement>(null);

  const client = useSdkClient();
  const [progress, setProgress] = useState<AnalysisProgress | null>(null);
  // Bumped to abandon an in-flight run (Stop, or another session picked).
  const runId = useRef(0);
  const running = progress !== null;

  // Labels the session a batch at a time so progress and decisions show as
  // they land instead of after one long request.
  async function runAnalysis() {
    if (!chatId) return;
    const id = ++runId.current;
    let merged: IntentSessionAnalysis | null = null;
    setAnalysis(null);
    setProgress({ done: 0, total: 0 });
    try {
      for (;;) {
        const batch: IntentSessionAnalysis =
          await client.intentPolicies.analyzeSession({
            analyzeSessionRequestBody: {
              chatId,
              evaluator,
              generation: merged?.generation,
              offset: merged?.assessments.length ?? 0,
              limit: ANALYSIS_BATCH,
            },
          });
        if (runId.current !== id) return;
        merged = mergeBatch(merged, batch);
        const done = merged.assessments.length;
        const total = analyzableCalls(batch.totals.calls);
        setAnalysis(merged);
        setProgress({ done, total });
        if (batch.assessments.length === 0 || done >= total) break;
      }
    } catch (err) {
      if (runId.current === id) handleError(err, { title: "Analysis failed" });
    } finally {
      if (runId.current === id) setProgress(null);
    }
  }

  function stopAnalysis() {
    runId.current++;
    setProgress(null);
  }

  function downloadReport() {
    if (!analysis) return;
    const now = new Date();
    const report = buildReport({
      analysis,
      guardrail: guardrailQuery.data,
      sessionName: chatTitle,
      generatedAt: now,
    });
    downloadBlob(
      new Blob([JSON.stringify(report, null, 2)], {
        type: "application/json",
      }),
      reportFilename(chatTitle, now),
    );
  }

  const upload = useUploadIntentTranscriptMutation({
    onSuccess: (data, vars) => {
      selectSession(data.chatId, vars.request.uploadTranscriptRequestBody.name);
      toast.success(
        `Uploaded ${data.messageCount} messages. Choose an evaluator and analyze.`,
      );
      void queryClient.invalidateQueries({
        queryKey: ["@gram/client", "chat"],
      });
    },
    onError: (err) => handleError(err, { title: "Upload failed" }),
  });

  // Picking a session only loads it; inference waits for Analyze.
  function selectSession(id: string, title: string) {
    setChatId(id);
    setChatTitle(title);
    setAnalysis(null);
    stopAnalysis();
  }

  async function uploadFile(file: File) {
    if (file.size > MAX_UPLOAD_BYTES) {
      toast.error("Transcripts must be 20 MB or smaller.");
      return;
    }
    upload.mutate({
      request: {
        uploadTranscriptRequestBody: {
          name: file.name,
          content: await file.text(),
        },
      },
    });
  }

  const setTab = (next: string) =>
    setParams((prev) => {
      const p = new URLSearchParams(prev);
      if (next === "guardrail") p.set("tab", "guardrail");
      else p.delete("tab");
      return p;
    });
  const selectRule = (id: string) =>
    setParams(
      (prev) => {
        const p = new URLSearchParams(prev);
        p.set("rule", id);
        return p;
      },
      { replace: true },
    );

  const onDrop = (e: DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    setDragging(false);
    const file = e.dataTransfer.files[0];
    if (file) void uploadFile(file);
  };

  const chats = chatsQuery.data?.chats ?? [];
  const items = chats.map((c) => ({
    value: c.id,
    label: c.title || "Untitled session",
    description: c.source,
  }));
  const selectedItem =
    items.find((i) => i.value === chatId) ??
    (chatId ? { value: chatId, label: chatTitle ?? "Session" } : undefined);

  const guardrailError = (
    <InlineEmptyState
      icon="triangle-alert"
      heading="Couldn't load the guardrail"
      action={
        <Button
          variant="secondary"
          onClick={() => void guardrailQuery.refetch()}
        >
          <Button.Text>Retry</Button.Text>
        </Button>
      }
    />
  );

  return (
    <Tabs value={tab} onValueChange={setTab} className="flex flex-col gap-6">
      <div className="border-b">
        <PageTabsList className="h-auto w-max gap-4 bg-transparent p-0">
          <PageTabsTrigger value="replay">Replay</PageTabsTrigger>
          <PageTabsTrigger value="guardrail">Guardrail</PageTabsTrigger>
        </PageTabsList>
      </div>

      {/* Both tabs stay mounted so the replay keeps its place across switches. */}
      <TabsContent
        value="replay"
        forceMount
        className={cn(
          "@container flex flex-col gap-6 data-[state=inactive]:hidden",
          dragging && "outline-1 outline-offset-4 outline-dashed",
        )}
        onDragOver={(e) => {
          e.preventDefault();
          setDragging(true);
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={onDrop}
      >
        <div className="flex flex-wrap items-center gap-2">
          <Combobox
            items={items}
            selected={selectedItem}
            onSelectionChange={(item) => selectSession(item.value, item.label)}
            onSearchChange={setSearch}
            searchPlaceholder="Search sessions"
            emptyMessage="No sessions match."
            className="w-80 max-w-full justify-between"
            contentClassName="w-[min(480px,calc(100vw-2rem))]"
          >
            <span className="truncate">
              {selectedItem?.label ?? "Choose a session"}
            </span>
          </Combobox>
          <Button
            variant="secondary"
            onClick={() => fileInput.current?.click()}
            disabled={upload.isPending}
          >
            <Button.Text>
              {upload.isPending ? "Uploading…" : "Upload transcript"}
            </Button.Text>
          </Button>
          <input
            ref={fileInput}
            type="file"
            accept=".json,.jsonl,application/json"
            className="hidden"
            onChange={(e) => {
              const file = e.target.files?.[0];
              e.target.value = "";
              if (file) void uploadFile(file);
            }}
          />
          <SegmentedControl
            ariaLabel="Evaluator"
            value={evaluator}
            onChange={setEvaluator}
            options={EVALUATOR_OPTIONS}
          />
          {running ? (
            <Button variant="secondary" onClick={stopAnalysis}>
              <Button.Text>Stop</Button.Text>
            </Button>
          ) : (
            <Button onClick={() => void runAnalysis()} disabled={!chatId}>
              <Button.Text>Analyze</Button.Text>
            </Button>
          )}
          <Button
            variant="tertiary"
            className="ml-auto"
            onClick={downloadReport}
            disabled={!analysis || running}
          >
            <Button.Text>Download report</Button.Text>
          </Button>
        </div>

        {guardrailQuery.isError ? (
          guardrailError
        ) : (
          <Results
            chatId={chatId}
            analysis={analysis}
            guardrail={guardrailQuery.data}
            progress={progress}
            focus={focus}
          />
        )}
      </TabsContent>

      <TabsContent
        value="guardrail"
        forceMount
        className="data-[state=inactive]:hidden"
      >
        {guardrailQuery.isError ? (
          guardrailError
        ) : !guardrailQuery.data ? (
          <InlineEmptyState icon="loader" heading="Loading the guardrail" />
        ) : (
          <GuardrailView
            guardrail={guardrailQuery.data}
            analysis={analysis}
            sessionName={chatTitle}
            selectedRuleId={params.get("rule")}
            onSelectRule={selectRule}
            onOpenCall={(call) => {
              setFocus({ key: `${call.messageId}:${call.toolCallId}` });
              setTab("replay");
            }}
          />
        )}
      </TabsContent>
    </Tabs>
  );
}

function Results({
  chatId,
  analysis,
  guardrail,
  progress,
  focus,
}: {
  chatId: string | undefined;
  analysis: IntentSessionAnalysis | null;
  guardrail: IntentGuardrail | undefined;
  progress: AnalysisProgress | null;
  focus: ReplayFocus | null;
}): JSX.Element {
  if (!chatId) {
    return (
      <InlineEmptyState
        icon="history"
        heading="Choose a session to replay"
        description="Pick a stored session, or drop a Claude Code, Pi, or messages transcript anywhere on this page."
      />
    );
  }
  return (
    <Session
      key={chatId}
      chatId={chatId}
      analysis={analysis}
      guardrail={guardrail}
      progress={progress}
      focus={focus}
    />
  );
}

// The transcript loads as soon as a session is picked; Analyze adds decisions.
function Session({
  chatId,
  analysis,
  guardrail,
  progress,
  focus,
}: {
  chatId: string;
  analysis: IntentSessionAnalysis | null;
  guardrail: IntentGuardrail | undefined;
  progress: AnalysisProgress | null;
  focus: ReplayFocus | null;
}): JSX.Element {
  const client = useSdkClient();
  const transcript = useQuery({
    queryKey: ["intent-policies", "transcript", chatId],
    queryFn: () => fetchFullTranscript(client, chatId),
    staleTime: Infinity,
    throwOnError: false,
  });
  const calls = useMemo(() => analysis?.assessments ?? [], [analysis]);
  const events = useMemo(
    () => buildSessionEvents(transcript.data ?? [], calls),
    [transcript.data, calls],
  );
  const failed = calls.filter((a) => callOutcome(a) === "failed").length;
  const callCount = events.filter((e) => e.kind === "tool_call").length;
  const running = progress !== null;
  const incomplete =
    analysis !== null &&
    analysis.totals.judged < analyzableCalls(analysis.totals.calls);

  if (transcript.isPending) {
    return <InlineEmptyState icon="loader" heading="Loading session" />;
  }
  if (events.length === 0) {
    return transcript.isError ? (
      <InlineEmptyState
        icon="triangle-alert"
        heading="Couldn't load this session"
        action={
          <Button variant="secondary" onClick={() => void transcript.refetch()}>
            <Button.Text>Retry</Button.Text>
          </Button>
        }
      />
    ) : (
      <InlineEmptyState
        icon="inbox"
        heading="Nothing to replay"
        description="This session has no messages."
      />
    );
  }

  return (
    <div className="flex flex-col gap-4">
      {analysis?.limitHit && (
        <Alert variant="warning">
          <AlertDescription>
            Only the first {analyzableCalls(analysis.totals.calls)} of{" "}
            {analysis.totals.calls} tool calls are analyzed. Longer sessions are
            cut off to bound cost.
          </AlertDescription>
        </Alert>
      )}
      {analysis && !running && incomplete && (
        <Alert variant="warning">
          <AlertDescription>
            The analysis stopped after {analysis.totals.judged} of{" "}
            {analyzableCalls(analysis.totals.calls)} calls. Analyze again to
            label the whole session.
          </AlertDescription>
        </Alert>
      )}
      {analysis && failed > 0 && (
        <Alert variant="error">
          <AlertDescription>
            The evaluator failed on {failed} of {analysis.totals.judged} calls.
            Those are shown hatched and count as blocks, because missing labels
            fail closed. Try again or switch evaluator.
          </AlertDescription>
        </Alert>
      )}
      {transcript.isError && (
        <Alert variant="warning">
          <AlertDescription>
            Couldn't load the session's messages, so only tool calls are shown.
          </AlertDescription>
        </Alert>
      )}
      {running ? (
        <ProgressCard
          done={progress.done}
          total={progress.total || analyzableCalls(callCount)}
          analysis={analysis}
        />
      ) : analysis ? (
        <Summary analysis={analysis} failed={failed} />
      ) : (
        <section
          aria-label="Analysis status"
          className="flex flex-wrap items-baseline gap-x-3 gap-y-1 border bg-card px-5 py-4"
        >
          <span className="text-display-xs font-thin tabular-nums">
            {callCount}
          </span>
          <Text muted>
            {callCount === 0
              ? "tool calls. Intent policies judge tool calls, and this session has none."
              : "tool calls. Choose an evaluator and press Analyze to see what the guardrail decides. Nothing runs until you do."}
          </Text>
        </section>
      )}
      <SessionViewer
        key={analysis ? `analysis:${analysis.generation}` : "transcript"}
        events={events}
        guardrail={guardrail}
        analysisState={running ? "running" : analysis ? "done" : "none"}
        focus={focus}
      />
    </div>
  );
}

/** Live progress of a batched run, with decisions so far on the bar. */
function ProgressCard({
  done,
  total,
  analysis,
}: {
  done: number;
  total: number;
  analysis: IntentSessionAnalysis | null;
}): JSX.Element {
  const counts = { permit: 0, clarify: 0, block: 0, failed: 0 };
  for (const a of analysis?.assessments ?? []) counts[callOutcome(a)]++;
  const remaining = Math.max(0, total - done);
  return (
    <section
      aria-label="Analysis progress"
      className="flex flex-wrap items-center gap-x-8 gap-y-3 border bg-card px-5 py-4"
    >
      <div className="flex items-baseline gap-2">
        <span className="text-display-xs font-thin tabular-nums">
          {done}
          <span className="text-muted-foreground"> / {total || "…"}</span>
        </span>
        <Text muted>calls labeled</Text>
      </div>
      <div className="flex min-w-0 flex-[1_1_20rem] flex-col gap-2">
        <div
          role="progressbar"
          aria-valuemin={0}
          aria-valuemax={total}
          aria-valuenow={done}
          className="flex h-2.5 gap-0.5"
        >
          {(["permit", "clarify", "block", "failed"] as const).map((k) =>
            counts[k] > 0 ? (
              <span
                key={k}
                className={OUTCOME_FILL[k]}
                style={{
                  flex: `${counts[k]} 1 0`,
                  ...(k === "failed" ? HATCH_STYLE : {}),
                }}
              />
            ) : null,
          )}
          {remaining > 0 && (
            <span
              className="animate-pulse bg-surface-tertiary-default"
              style={{ flex: `${remaining} 1 0` }}
            />
          )}
        </div>
        <Text small muted>
          Labeling each call, then checking it against every contract. Decisions
          appear on the timeline as they land.
        </Text>
      </div>
    </section>
  );
}

function Summary({
  analysis,
  failed,
}: {
  analysis: IntentSessionAnalysis;
  failed: number;
}): JSX.Element {
  const { totals, evaluator } = analysis;
  const calls = analysis.assessments;
  const first = calls[0]?.occurredAt.getTime() ?? 0;
  const last = calls[calls.length - 1]?.occurredAt.getTime() ?? first;
  const segments = [
    { key: "permit", n: totals.permitted, label: "permitted" },
    { key: "clarify", n: totals.clarified, label: "ask a person" },
    { key: "block", n: totals.blocked - failed, label: "blocked" },
    { key: "failed", n: failed, label: "evaluator failed" },
  ] as const;

  return (
    <section
      aria-label="Summary"
      className="flex flex-wrap items-center gap-x-8 gap-y-4 border bg-card px-5 py-4"
    >
      <div className="flex items-baseline gap-2">
        <span className="text-display-xs font-thin tabular-nums">
          {totals.calls}
        </span>
        <Text muted>tool calls over {formatDuration(last - first)}</Text>
      </div>
      <div className="flex min-w-0 flex-[1_1_20rem] flex-col gap-2">
        <div className="flex h-2.5 gap-0.5">
          {segments.map((s) =>
            s.n > 0 ? (
              <span
                key={s.key}
                className={OUTCOME_FILL[s.key]}
                style={{
                  flex: `${s.n} 1 0`,
                  ...(s.key === "failed" ? HATCH_STYLE : {}),
                }}
              />
            ) : null,
          )}
        </div>
        <div className="flex flex-wrap gap-x-5 gap-y-1 text-xs text-muted-foreground">
          {segments
            .filter((s) => s.key !== "failed" || s.n > 0)
            .map((s) => (
              <span key={s.key}>
                <span className="font-semibold text-foreground tabular-nums">
                  {s.n}
                </span>{" "}
                {s.label}
              </span>
            ))}
        </div>
      </div>
      <div className="flex flex-col gap-0.5 font-mono text-2xs text-muted-foreground">
        <span>
          {evaluator.model} via {evaluator.provider} · prompt{" "}
          {evaluator.promptVersion}
        </span>
        <span>
          guardrail {analysis.guardrailVersion} ·{" "}
          {Math.round(totals.latencyMs / 1000)}s evaluator time ·{" "}
          {formatCost(totals.costUsd)}
        </span>
      </div>
    </section>
  );
}
