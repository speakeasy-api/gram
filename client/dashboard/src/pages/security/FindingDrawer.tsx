import { IdentityLink } from "@/components/identity-link";
import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Skeleton } from "@/components/ui/Skeleton";
import { useRBAC } from "@/hooks/useRBAC";
import { identityRefForUserKey } from "@/lib/identity-urn";
import { cn } from "@/lib/utils";
import { ChatDetailSheet } from "@/pages/chatLogs/ChatDetailPanel";
import { useRoutes } from "@/routes";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { useRiskListResults } from "@gram/client/react-query/riskListResults.js";
import { ArrowRight, ChevronDown, ChevronUp, Link2, X } from "lucide-react";
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import { CallPath } from "./CallPath";
import { FindingContext } from "./FindingContext";
import {
  DRAWER_FOOTNOTE,
  DrawerSection,
  FactGrid,
} from "./finding-drawer-parts";
import {
  chatFindingDetail,
  FINDING_KIND_LABEL,
  findingKind,
  findingMessageKind,
  isChatLikeKind,
  type FindingKind,
} from "./finding-kind";
import {
  formatCallTime,
  siblingLocationLabel,
  withArticle,
} from "./finding-drawer-format";
import { FindingPayload } from "./FindingPayload";
import { useExecutionPayload } from "./use-execution-payload";
import {
  mcpFindingTargetName,
  mediationSurfaceLabel,
  type MCPFindingNames,
} from "./mcp-finding-context";
import { RULE_CATEGORY_META } from "./policy-data";
import { useRevealAll } from "./reveal-all-context";
import { enforcementOutcomeLabel, isBlockingOutcome } from "./risk-outcome";
import {
  displayedScoreRating,
  SEVERITY_SWATCH,
  SEVERITY_TEXT,
} from "./risk-severity";
import {
  getCategoryForFinding,
  getRuleTitleFallback,
  isJudgeSource,
  isRationaleSource,
  SEVERITY_RATING_LABEL,
  type SeverityRating,
} from "./risk-utils";
import { shadowServerFacts } from "./shadow-identifier";
import { isRedactionFingerprint, REVEAL_SCOPE } from "./unmask";
import { SuppressMenu } from "./watchdog/SuppressMenu";

const SQUARE_ICON_BUTTON =
  "hover:bg-muted inline-flex size-7 items-center justify-center border disabled:pointer-events-none disabled:opacity-40";

function categoryLabel(result: RiskResult): string {
  const category = getCategoryForFinding(result.source, result.ruleId);
  return RULE_CATEGORY_META[category ?? "custom"].label;
}

function findingTitle(
  result: RiskResult,
  kind: FindingKind,
  serverName: string,
): string {
  const messageKind = findingMessageKind(result).toLowerCase();
  switch (kind) {
    case "mcp":
      return `${getRuleTitleFallback(result.ruleId)} in ${withArticle(serverName)} tool ${result.phase === "response" ? "result" : "call"}`;
    case "chat":
      return `${getRuleTitleFallback(result.ruleId)} in ${withArticle(messageKind)}`;
    case "judge":
      return `${categoryLabel(result)} in ${withArticle(messageKind)}`;
    case "analyzer":
      return `${categoryLabel(result)} in ${withArticle(messageKind)}`;
    case "shadow":
      return "Unregistered MCP server called";
  }
}

function findingContextLine(result: RiskResult, kind: FindingKind): string {
  switch (kind) {
    case "mcp": {
      const outcome = enforcementOutcomeLabel(result.enforcementOutcome);
      const phase = result.phase ?? "request";
      return outcome ? `${outcome} at ${phase}` : `Scanned at ${phase}`;
    }
    case "chat":
      return `matched in session “${result.chatTitle ?? "Untitled"}”`;
    case "judge":
      return "flagged by a prompt-based judge";
    case "analyzer":
      return "flagged by the risk analyzer, no spans reported";
    case "shadow":
      return `${result.toolName ?? "A tool"} called from a server outside the MCP inventory`;
  }
}

function confidenceText(result: RiskResult): string {
  return (result.confidence ?? 0).toFixed(2);
}

function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  return (
    target.isContentEditable ||
    ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName)
  );
}

export type FindingDrawerProps = {
  /** The `?finding=` id; null closes the drawer. */
  findingId: string | null;
  /** The loaded, visible list the drawer pages through. */
  results: RiskResult[];
  policyNameById: Map<string, string>;
  policyScoreById: Map<string, number>;
  mcpFindingNames: MCPFindingNames;
  /** The `?chat_id=` transcript open on top of the drawer. */
  transcriptChatId: string | null;
  onSelect: (findingId: string | null) => void;
  onOpenTranscript: (chatId: string | null) => void;
  onDismiss: (result: RiskResult) => void;
  onSetupExclusion: (result: RiskResult) => void;
};

export function FindingDrawer({
  findingId,
  results,
  transcriptChatId,
  onSelect,
  onOpenTranscript,
  ...rest
}: FindingDrawerProps): JSX.Element {
  const loaded = findingId
    ? results.find((r) => r.id === findingId)
    : undefined;
  // A deep link to a finding that isn't on a loaded page resolves on its own.
  const deepLink = useRiskListResults(
    { resultId: findingId ?? "", limit: 1 },
    undefined,
    { enabled: findingId !== null && !loaded, throwOnError: false },
  );
  const result =
    loaded ?? deepLink.data?.results.find((r) => r.id === findingId) ?? null;
  const index = loaded ? results.indexOf(loaded) : -1;
  const prev = index > 0 ? results[index - 1] : undefined;
  const next = index >= 0 ? results[index + 1] : undefined;

  const open = findingId !== null;
  useEffect(() => {
    if (!open || transcriptChatId) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey || isTypingTarget(e.target)) {
        return;
      }
      if (e.key === "j" && next) onSelect(next.id);
      if (e.key === "k" && prev) onSelect(prev.id);
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [open, transcriptChatId, next, prev, onSelect]);

  return (
    <Sheet
      open={open}
      onOpenChange={(isOpen) => {
        if (!isOpen) onSelect(null);
      }}
    >
      <SheetContent
        side="right"
        className="w-full gap-0 overflow-y-auto sm:max-w-3xl"
        showCloseButton={false}
      >
        {result ? (
          <FindingDetail
            result={result}
            position={index >= 0 ? `${index + 1} of ${results.length}` : null}
            onPrev={prev ? () => onSelect(prev.id) : undefined}
            onNext={next ? () => onSelect(next.id) : undefined}
            onClose={() => onSelect(null)}
            onSelect={onSelect}
            onOpenTranscript={onOpenTranscript}
            loadedResults={results}
            {...rest}
          />
        ) : (
          <FindingPlaceholder
            loading={deepLink.isLoading}
            onClose={() => onSelect(null)}
          />
        )}
        {/* Nested in this sheet so closing it doesn't read as an outside
            click and close the drawer too. */}
        <ChatDetailSheet
          chatId={open ? transcriptChatId : null}
          focusedMessageId={result?.chatMessageId}
          onClose={() => onOpenTranscript(null)}
          onDelete={() => onOpenTranscript(null)}
          riskFocus
        />
      </SheetContent>
    </Sheet>
  );
}

function FindingPlaceholder({
  loading,
  onClose,
}: {
  loading: boolean;
  onClose: () => void;
}): JSX.Element {
  return (
    <>
      <SheetHeader className="flex-row items-start justify-between">
        <div className="flex flex-col gap-1.5">
          <SheetTitle className="text-lg font-normal">
            {loading ? "Loading finding" : "Finding not found"}
          </SheetTitle>
          <SheetDescription>
            {loading
              ? "Fetching the finding from this link."
              : "It may have been suppressed, or the link points to another project."}
          </SheetDescription>
        </div>
        <button
          type="button"
          aria-label="Close"
          className="opacity-70 hover:opacity-100"
          onClick={onClose}
        >
          <X className="size-4" />
        </button>
      </SheetHeader>
      {loading && (
        <div className="px-4">
          <Skeleton>
            <div className="h-24 w-full" />
            <div className="h-40 w-full" />
          </Skeleton>
        </div>
      )}
    </>
  );
}

// Starts at the page's reveal-all value, resets to it whenever the finding
// changes, and follows the page toggle when it fires.
function useDrawerReveal(
  findingId: string,
): [boolean, (next: boolean) => void] {
  const ctx = useRevealAll();
  const revealAll = ctx?.revealAll ?? false;
  const generation = ctx?.generation;
  const [state, setState] = useState({ findingId, revealed: revealAll });
  if (state.findingId !== findingId) {
    setState({ findingId, revealed: revealAll });
  }
  const lastGeneration = useRef(generation);
  useEffect(() => {
    if (lastGeneration.current === generation) return;
    lastGeneration.current = generation;
    setState((s) => ({ ...s, revealed: revealAll }));
  }, [generation, revealAll]);
  const setRevealed = useCallback(
    (revealed: boolean) => setState((s) => ({ ...s, revealed })),
    [],
  );
  return [
    state.findingId === findingId ? state.revealed : revealAll,
    setRevealed,
  ];
}

function FindingDetail({
  result,
  position,
  onPrev,
  onNext,
  onClose,
  onSelect,
  onOpenTranscript,
  loadedResults,
  policyNameById,
  policyScoreById,
  mcpFindingNames,
  onDismiss,
  onSetupExclusion,
}: {
  result: RiskResult;
  position: string | null;
  onPrev?: () => void;
  onNext?: () => void;
  onClose: () => void;
  onSelect: (findingId: string | null) => void;
  onOpenTranscript: (chatId: string | null) => void;
  loadedResults: RiskResult[];
  policyNameById: Map<string, string>;
  policyScoreById: Map<string, number>;
  mcpFindingNames: MCPFindingNames;
  onDismiss: (result: RiskResult) => void;
  onSetupExclusion: (result: RiskResult) => void;
}): JSX.Element {
  const routes = useRoutes();
  const { hasScope } = useRBAC();
  const canReveal = hasScope(REVEAL_SCOPE);
  const kind = findingKind(result);
  const chatLike = isChatLikeKind(kind);
  const [revealed, setRevealed] = useDrawerReveal(result.id);

  const score = policyScoreById.get(result.policyId);
  const scored = score != null ? displayedScoreRating(score) : null;
  const rating: SeverityRating | null = scored?.rating ?? null;
  const policyName = policyNameById.get(result.policyId) ?? "Unknown policy";
  const serverName = mcpFindingTargetName(result, mcpFindingNames);
  const surfaceLabel =
    mediationSurfaceLabel(result.mediationSurface) ?? "MCP gateway";

  const executionQuery = useRiskListResults(
    { executionId: result.executionId ?? "", limit: 100 },
    undefined,
    {
      enabled: kind === "mcp" && Boolean(result.executionId),
      throwOnError: false,
    },
  );
  const chatQuery = useRiskListResults(
    { chatId: result.chatId ?? "", limit: 200 },
    undefined,
    { enabled: chatLike && Boolean(result.chatId), throwOnError: false },
  );
  let chatResults: RiskResult[] | undefined;
  if (chatQuery.data) chatResults = chatQuery.data.results;
  else if (!chatQuery.isLoading) chatResults = [];

  let siblings: RiskResult[] = [];
  if (kind === "mcp" && result.executionId) {
    siblings = executionQuery.data?.results ?? [result];
  } else if (chatLike && result.chatMessageId) {
    siblings = (chatResults ?? loadedResults).filter(
      (r) => r.chatMessageId === result.chatMessageId,
    );
  }

  const payload = useExecutionPayload(kind === "mcp" ? result : null);
  const { reveal: revealPayload } = payload;
  const payloadFailed = payload.isError;
  // A failed reveal waits for Retry rather than looping.
  useEffect(() => {
    if (kind === "mcp" && revealed && canReveal && !payloadFailed) {
      revealPayload();
    }
  }, [kind, revealed, canReveal, payloadFailed, revealPayload]);

  const copyLink = async () => {
    const url = new URL(window.location.href);
    url.searchParams.set("finding", result.id);
    url.searchParams.delete("chat_id");
    try {
      await navigator.clipboard.writeText(url.toString());
      toast.success("Link copied to clipboard");
    } catch {
      toast.error("Failed to copy link");
    }
  };

  const suppress = () => {
    onDismiss(result);
    onClose();
  };

  const outcome = enforcementOutcomeLabel(result.enforcementOutcome);
  const shadowVerbatim =
    kind === "shadow" &&
    result.matchRedacted !== undefined &&
    !isRedactionFingerprint(result.matchRedacted);

  return (
    <>
      <SheetHeader className="gap-1.5 pb-5">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-baseline gap-2">
            {scored && rating && (
              <>
                <span
                  className={cn(
                    "font-display text-2xl leading-none font-thin tabular-nums",
                    SEVERITY_TEXT[rating],
                  )}
                >
                  {scored.displayed.toFixed(1)}
                </span>
                <span className="text-muted-foreground text-xs uppercase">
                  {SEVERITY_RATING_LABEL[rating]}
                </span>
              </>
            )}
            <span className="text-muted-foreground ml-2 border px-1.5 py-px font-mono text-[10px] tracking-[0.1em] uppercase">
              {FINDING_KIND_LABEL[kind]}
            </span>
          </div>
          <div className="flex items-center gap-1">
            {position && (
              <span className="text-muted-foreground mr-1.5 font-mono text-[11px]">
                {position}
              </span>
            )}
            <button
              type="button"
              aria-label="Previous finding"
              title="Previous finding (k)"
              className={SQUARE_ICON_BUTTON}
              disabled={!onPrev}
              onClick={onPrev}
            >
              <ChevronUp className="size-4" />
            </button>
            <button
              type="button"
              aria-label="Next finding"
              title="Next finding (j)"
              className={SQUARE_ICON_BUTTON}
              disabled={!onNext}
              onClick={onNext}
            >
              <ChevronDown className="size-4" />
            </button>
            <button
              type="button"
              aria-label="Close"
              className="ml-2 inline-flex size-7 items-center justify-center opacity-70 hover:opacity-100"
              onClick={onClose}
            >
              <X className="size-4" />
            </button>
          </div>
        </div>
        <SheetTitle className="mt-1 text-lg leading-snug font-normal">
          {findingTitle(result, kind, serverName)}
        </SheetTitle>
        <SheetDescription className="text-pretty">
          {categoryLabel(result)} · {policyName} v{result.policyVersion} ·{" "}
          {findingContextLine(result, kind)}
        </SheetDescription>
      </SheetHeader>

      <div className="flex flex-col gap-6 px-4 pb-8">
        <div className="flex flex-wrap items-center gap-2">
          {isJudgeSource(result.source) ? (
            <Button variant="primary" onClick={suppress}>
              <Button.Text>Suppress</Button.Text>
            </Button>
          ) : (
            <SuppressMenu
              variant="primary"
              onSuppressOnce={suppress}
              onCreateRule={() => onSetupExclusion(result)}
            />
          )}
          {(chatLike || kind === "shadow") && result.chatId && (
            <Button
              variant="secondary"
              onClick={() => onOpenTranscript(result.chatId ?? null)}
            >
              <Button.Text>Open transcript</Button.Text>
              <Button.RightIcon>
                <ArrowRight className="size-4" />
              </Button.RightIcon>
            </Button>
          )}
          {kind === "shadow" && (
            <Button asChild variant="secondary">
              <Link to={routes.shadowAI.mcps.href()}>
                Open Shadow MCP inventory
                <ArrowRight className="size-4" />
              </Link>
            </Button>
          )}
          <Button variant="tertiary" onClick={() => void copyLink()}>
            <Button.LeftIcon>
              <Link2 className="size-4" />
            </Button.LeftIcon>
            <Button.Text>Copy link</Button.Text>
          </Button>
          {kind === "mcp" && outcome && (
            <span
              className={cn(
                "ml-auto inline-flex h-7 items-center border px-2.5 font-mono text-[11px] tracking-[0.1em] uppercase",
                isBlockingOutcome(result.enforcementOutcome)
                  ? "border-foreground text-foreground"
                  : "border-border text-muted-foreground",
              )}
            >
              {outcome}
            </span>
          )}
        </div>

        {isRationaleSource(result.source) && result.description?.trim() && (
          <DrawerSection label="Why this was flagged">
            <div className="bg-card flex flex-col gap-2.5 border p-4">
              <p className="text-sm leading-relaxed text-pretty">
                {result.description.trim()}
              </p>
              <span className={DRAWER_FOOTNOTE}>
                {isJudgeSource(result.source)
                  ? `Judge verdict · conf ${confidenceText(result)} · not gated, model-authored`
                  : `Analyzer verdict · conf ${confidenceText(result)} · no spans, nothing to reveal`}
              </span>
            </div>
          </DrawerSection>
        )}

        {kind === "shadow" && result.matchRedacted && (
          <ShadowIdentifier
            identifier={result.matchRedacted}
            verbatim={shadowVerbatim}
          />
        )}

        {kind === "mcp" && (
          <DrawerSection
            label="Call path"
            aside={
              <span className={DRAWER_FOOTNOTE}>
                {result.mcpMethod ?? "tools/call"} ·{" "}
                {formatCallTime(result.createdAt)}
              </span>
            }
          >
            <CallPath
              result={result}
              siblings={siblings}
              serverName={serverName}
              surfaceLabel={surfaceLabel}
              rating={rating}
            />
          </DrawerSection>
        )}

        {kind === "mcp" && (
          <FindingPayload
            result={result}
            siblings={siblings}
            revealed={revealed}
            canReveal={canReveal}
            payload={payload}
            onToggleReveal={() => setRevealed(!revealed)}
            onSelect={(id) => onSelect(id)}
          />
        )}

        {chatLike && (
          <FindingContext
            key={result.id}
            result={result}
            kind={kind}
            chatResults={chatResults}
            revealed={revealed}
            canReveal={canReveal}
            rating={rating}
            onToggleReveal={() => setRevealed(!revealed)}
            onRequestReveal={() => setRevealed(true)}
          />
        )}

        {siblings.length > 1 && (
          <SiblingFindings
            label={`Findings in this ${kind === "mcp" ? "call" : "message"} · ${siblings.length}`}
            siblings={siblings}
            currentId={result.id}
            rating={rating}
            onSelect={(id) => onSelect(id)}
          />
        )}

        <DrawerSection label="Details">
          <FactGrid
            facts={detailFacts(result, kind, {
              policyName,
              serverName,
              surfaceLabel,
            })}
          />
        </DrawerSection>
      </div>
    </>
  );
}

function ShadowIdentifier({
  identifier,
  verbatim,
}: {
  identifier: string;
  verbatim: boolean;
}): JSX.Element {
  const facts = verbatim ? shadowServerFacts(identifier) : null;
  return (
    <DrawerSection label="Server identifier">
      <div className="bg-card border">
        <div className="border-b-muted flex items-center justify-between gap-3 border-b px-4 py-3.5">
          <span className="font-mono text-sm break-all">{identifier}</span>
          {verbatim && (
            <CopyButton text={identifier} size="sm" tooltip="Copy identifier" />
          )}
        </div>
        {facts && (
          <FactGrid
            minWidth="160px"
            bordered={false}
            facts={[
              { label: "Transport", value: facts.transport },
              {
                label: "Package",
                value: facts.pkg ?? "-",
                title: facts.pkg ?? undefined,
              },
              { label: "Version", value: facts.version ?? "-" },
              { label: "Inventory", value: "Not registered" },
            ]}
          />
        )}
      </div>
      {verbatim && (
        <span className={DRAWER_FOOTNOTE}>
          Shown verbatim: the identifier names a server, not user content.
        </span>
      )}
    </DrawerSection>
  );
}

function SiblingFindings({
  label,
  siblings,
  currentId,
  rating,
  onSelect,
}: {
  label: string;
  siblings: RiskResult[];
  currentId: string;
  rating: SeverityRating | null;
  onSelect: (id: string) => void;
}): JSX.Element {
  return (
    <DrawerSection label={label}>
      <div className="bg-card border">
        {siblings.map((sibling, i) => {
          const current = sibling.id === currentId;
          const where = siblingLocationLabel(sibling);
          return (
            <button
              key={sibling.id}
              type="button"
              onClick={() => onSelect(sibling.id)}
              className={cn(
                "hover:bg-muted/50 grid w-full grid-cols-[18px_minmax(0,1fr)_auto_auto] items-center gap-3 px-3 py-2.5 text-left",
                i > 0 && "border-t-muted border-t",
                current && "bg-[var(--color-feedback-orange-400)]/8",
              )}
            >
              <span
                className={cn(
                  "size-2",
                  current && rating ? SEVERITY_SWATCH[rating] : "bg-border",
                )}
              />
              <span className="flex min-w-0 flex-col gap-0.5">
                <span className="text-[13px] font-normal">
                  {isJudgeSource(sibling.source)
                    ? categoryLabel(sibling)
                    : getRuleTitleFallback(sibling.ruleId)}
                </span>
                {where && (
                  <span className="text-muted-foreground truncate font-mono text-[11px]">
                    {where}
                  </span>
                )}
              </span>
              <span className="text-muted-foreground font-mono text-[11px]">
                conf {confidenceText(sibling)}
              </span>
              <span className="text-muted-foreground min-w-16 text-right font-mono text-[10px] tracking-[0.1em] uppercase">
                {current ? "Viewing" : ""}
              </span>
            </button>
          );
        })}
      </div>
    </DrawerSection>
  );
}

function UserValue({ userId }: { userId: string | undefined }): JSX.Element {
  return (
    <IdentityLink identifier={identityRefForUserKey(userId)}>
      {userId ?? "-"}
    </IdentityLink>
  );
}

function detailFacts(
  result: RiskResult,
  kind: FindingKind,
  names: { policyName: string; serverName: string; surfaceLabel: string },
): { label: string; value: ReactNode; title?: string }[] {
  const timestamp = result.createdAt.toLocaleString();
  const policy = `${names.policyName} · v${result.policyVersion}`;
  const user = {
    label: "User",
    value: <UserValue userId={result.userId} />,
    title: result.userId,
  };
  if (kind === "mcp") {
    return [
      { label: "MCP server", value: names.serverName, title: names.serverName },
      { label: "Tool", value: result.toolName ?? "-", title: result.toolName },
      { label: "Surface", value: names.surfaceLabel },
      {
        label: "Phase",
        value:
          result.phase === "response"
            ? "Response (result)"
            : "Request (arguments)",
      },
      user,
      { label: "Policy", value: policy, title: policy },
      { label: "Rule ID", value: result.ruleId ?? "-", title: result.ruleId },
      {
        label: "Execution",
        value: result.executionId ?? "-",
        title: result.executionId,
      },
      { label: "Timestamp", value: timestamp },
    ];
  }
  const message = chatFindingDetail(result);
  return [
    {
      label: "Session",
      value: result.chatTitle ?? "Untitled",
      title: result.chatTitle,
    },
    ...(message ? [{ label: "Message", value: message, title: message }] : []),
    user,
    { label: "Policy", value: policy, title: policy },
    ...(isJudgeSource(result.source)
      ? []
      : [
          {
            label: "Rule ID",
            value: result.ruleId ?? "-",
            title: result.ruleId,
          },
        ]),
    { label: "Confidence", value: confidenceText(result) },
    { label: "Timestamp", value: timestamp },
  ];
}
