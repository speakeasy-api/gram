import { InlineEmptyState } from "@/components/inline-empty-state";
import { Page } from "@/components/page-layout";
import { StatRow } from "@/components/stat-row";
import { Alert, AlertDescription } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Combobox } from "@/components/ui/Combobox";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Text } from "@/components/ui/Text";
import { useIsPlatformAdmin } from "@/contexts/Auth";
import { handleError } from "@/lib/errors";
import { cn } from "@/lib/utils";
import { Evaluator } from "@gram/client/models/components/analyzesessionrequestbody.js";
import type { IntentGuardrail } from "@gram/client/models/components/intentguardrail.js";
import type { IntentSessionAnalysis } from "@gram/client/models/components/intentsessionanalysis.js";
import { SortBy, SortOrder } from "@gram/client/models/operations/listchats";
import { useAnalyzeIntentSessionMutation } from "@gram/client/react-query/analyzeIntentSession.js";
import { useIntentGuardrail } from "@gram/client/react-query/intentGuardrail.js";
import { useListChats } from "@gram/client/react-query/listChats.js";
import { useUploadIntentTranscriptMutation } from "@gram/client/react-query/uploadIntentTranscript.js";
import { keepPreviousData, useQueryClient } from "@tanstack/react-query";
import { useDeferredValue, useRef, useState, type DragEvent } from "react";
import { toast } from "sonner";
import { DecisionPanel } from "./DecisionPanel";
import { GuardrailSheet } from "./GuardrailSheet";
import { callOutcome, formatCost } from "./model";
import { Timeline } from "./Timeline";

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
  const guardrailQuery = useIntentGuardrail();
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
  const [selected, setSelected] = useState(0);
  const [guardrailOpen, setGuardrailOpen] = useState(false);
  const [dragging, setDragging] = useState(false);
  const fileInput = useRef<HTMLInputElement>(null);

  const analyze = useAnalyzeIntentSessionMutation({
    onSuccess: (data) => {
      setAnalysis(data);
      setSelected(0);
    },
    onError: (err) => handleError(err, { title: "Analysis failed" }),
  });

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
    analyze.reset();
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

  return (
    <div
      className={cn(
        "@container flex flex-col gap-6",
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
        <Button
          onClick={() => {
            if (!chatId) return;
            analyze.mutate({
              request: { analyzeSessionRequestBody: { chatId, evaluator } },
            });
          }}
          disabled={!chatId || analyze.isPending}
        >
          <Button.Text>
            {analyze.isPending ? "Analyzing…" : "Analyze"}
          </Button.Text>
        </Button>
        <Button
          variant="tertiary"
          className="ml-auto"
          onClick={() => setGuardrailOpen(true)}
          disabled={!guardrailQuery.data}
        >
          <Button.Text>View guardrail</Button.Text>
        </Button>
      </div>

      {guardrailQuery.isError ? (
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
      ) : (
        <Results
          chatId={chatId}
          analysis={analysis}
          guardrail={guardrailQuery.data}
          pending={analyze.isPending}
          selected={selected}
          onSelect={setSelected}
        />
      )}

      {guardrailQuery.data && (
        <GuardrailSheet
          guardrail={guardrailQuery.data}
          open={guardrailOpen}
          onOpenChange={setGuardrailOpen}
        />
      )}
    </div>
  );
}

function Results({
  chatId,
  analysis,
  guardrail,
  pending,
  selected,
  onSelect,
}: {
  chatId: string | undefined;
  analysis: IntentSessionAnalysis | null;
  guardrail: IntentGuardrail | undefined;
  pending: boolean;
  selected: number;
  onSelect: (index: number) => void;
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
  if (pending) {
    return (
      <InlineEmptyState
        icon="loader"
        heading="Labeling tool calls"
        description="Each call is labeled by the evaluator, then checked against every contract."
      />
    );
  }
  if (!analysis || !guardrail) {
    return (
      <InlineEmptyState
        icon="play"
        heading="Session loaded"
        description="Choose an evaluator and press Analyze. Nothing runs until you do."
      />
    );
  }
  if (analysis.assessments.length === 0) {
    return (
      <InlineEmptyState
        icon="inbox"
        heading="No tool calls in this session"
        description="Intent policies judge tool calls, and this session has none."
      />
    );
  }

  const { totals } = analysis;
  const failed = analysis.assessments.filter(
    (a) => callOutcome(a) === "failed",
  ).length;
  const call =
    analysis.assessments[Math.min(selected, analysis.assessments.length - 1)];

  return (
    <div className="flex flex-col gap-4">
      {analysis.limitHit && (
        <Alert variant="warning">
          <AlertDescription>
            Only the first {totals.calls} tool calls were analyzed. Longer
            sessions are cut off to bound cost.
          </AlertDescription>
        </Alert>
      )}
      {failed > 0 && (
        <Alert variant="error">
          <AlertDescription>
            The evaluator failed on {failed} of {totals.calls} calls. Those are
            shown hatched and count as blocks, because missing labels fail
            closed. Try again or switch evaluator.
          </AlertDescription>
        </Alert>
      )}
      <StatRow
        metrics={[
          { label: "Tool calls", value: totals.calls, tone: "information" },
          { label: "Permitted", value: totals.permitted, tone: "neutral" },
          {
            label: "Clarify",
            value: totals.clarified,
            tone: totals.clarified > 0 ? "warning" : "neutral",
          },
          {
            label: "Blocked",
            value: totals.blocked,
            tone: totals.blocked > 0 ? "destructive" : "neutral",
          },
          {
            label: "Cost",
            value: formatCost(totals.costUsd),
            tone: "neutral",
            description: `${analysis.evaluator.model} · ${Math.round(totals.latencyMs / 1000)}s total`,
          },
        ]}
      />
      <Timeline
        calls={analysis.assessments}
        questions={guardrail.questions}
        selected={selected}
        onSelect={onSelect}
      />
      <Legend />
      {call && <DecisionPanel call={call} guardrail={guardrail} />}
      <Text small muted>
        Evaluator {analysis.evaluator.evaluator} via{" "}
        {analysis.evaluator.provider}, model {analysis.evaluator.model}, prompt{" "}
        {analysis.evaluator.promptVersion}, guardrail{" "}
        {analysis.guardrailVersion}.
      </Text>
    </div>
  );
}

function Legend(): JSX.Element {
  const swatch = "inline-block size-3 border align-middle";
  return (
    <div className="flex flex-wrap gap-x-5 gap-y-1 text-xs text-muted-foreground">
      <span>
        <span className={cn(swatch, "bg-muted")} /> Permit
      </span>
      <span>
        <span className={cn(swatch, "bg-warning-default")} /> Clarify: asks a
        person first
      </span>
      <span>
        <span className={cn(swatch, "bg-destructive-default")} /> Block
      </span>
      <span>
        <span
          className={swatch}
          style={{
            backgroundImage:
              "repeating-linear-gradient(135deg, currentColor 0 1px, transparent 1px 5px)",
          }}
        />{" "}
        Evaluator failed
      </span>
      <span>Dark cells: yes, shaded by confidence. Bars: effect classes.</span>
    </div>
  );
}
