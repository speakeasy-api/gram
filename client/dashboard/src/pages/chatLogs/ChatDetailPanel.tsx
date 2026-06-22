import { format } from "date-fns";
import { ArrowLeft, ChevronDown, SlidersHorizontal } from "lucide-react";
import {
  type ReactNode,
  useCallback,
  useEffect,
  useMemo,
  useState,
} from "react";
import {
  Badge,
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  Icon,
} from "@speakeasy-api/moonshine";
import type { RiskResult } from "@gram/client/models/components";
import { useSearchLogsMutation } from "@gram/client/react-query";
import { useRiskListResults } from "@gram/client/react-query/riskListResults.js";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetTitle,
} from "@/components/ui/sheet";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Dialog } from "@/components/ui/dialog";
import { HookSourceIcon } from "@/pages/hooks/HookSourceIcon";
import { useRBAC } from "@/hooks/useRBAC";
import { useIsAdmin } from "@/contexts/Auth";
import {
  ExclusionEditor,
  type ExclusionSheetState,
} from "@/pages/security/exclusion-sheet";
import { useChatTranscript } from "./useChatTranscript";
import { useChatRiskTranscript } from "./useChatRiskTranscript";
import { CreateExclusionContext } from "./exclusionContext";
import { findingToExclusionState } from "./chatRiskHelpers";
import {
  ChatTranscript,
  type RowContext,
  type TranscriptPagination,
} from "./ChatTranscript";
import { buildDisplayItems, buildTranscript } from "./transcript";
import {
  buildClaudeToolUsageByToolUseId,
  buildClaudeUsageByMessageId,
  formatTokenCount,
  formatUsageCost,
} from "./claudeUsage";
import { filterPanelTelemetryLogs, filterToolLogs } from "./chatLogFilters";
import { TelemetryLogsView, ToolCallsView } from "./chatLogViews";
import { exportTraceDataAsJson } from "./chatExport";

const PANEL_TELEMETRY_LOG_LIMIT = 100;

interface ChatDetailPanelProps {
  chatId: string;
  onClose: () => void;
  onDelete: (chatId: string) => void;
  /** Risk-focused view: collapse the transcript to the flagged messages plus a
   * few of context either side, expandable via "show more". Implies dimming. */
  riskFocus?: boolean;
  /** Dim non-flagged rows to spotlight findings, without the risk windowing.
   * Use from risk-filtered lists (e.g. Agent Sessions filtered to has_risk). */
  dimNonRisk?: boolean;
}

interface ChatDetailSheetProps extends Omit<ChatDetailPanelProps, "chatId"> {
  chatId: string | null;
}

type ViewMode = "chat" | "logs" | "tools" | "exclusion";

// Identity for a finding, used to optimistically hide it the moment an exclusion
// is created for it (the server reconcile is async, so a refetch lags).
function findingKey(r: RiskResult): string {
  return `${r.source}|${r.ruleId ?? ""}|${r.match ?? ""}`;
}

function getTraceId(chatId: string): string {
  return `trace-${chatId.slice(0, 3)}`;
}

function totalTokensFor(chat: {
  totalTokens?: number;
  totalInputTokens?: number;
  totalOutputTokens?: number;
}): number {
  if (chat.totalTokens && chat.totalTokens > 0) return chat.totalTokens;
  return (chat.totalInputTokens || 0) + (chat.totalOutputTokens || 0);
}

export function ChatDetailSheet({
  chatId,
  onClose,
  onDelete,
  riskFocus,
  dimNonRisk,
}: ChatDetailSheetProps): JSX.Element {
  return (
    <Sheet
      open={Boolean(chatId)}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <SheetContent
        className="w-[min(760px,calc(100vw-2rem))] sm:max-w-[760px]"
        showCloseButton={false}
      >
        {chatId && (
          <ChatDetailPanel
            chatId={chatId}
            onClose={onClose}
            onDelete={onDelete}
            riskFocus={riskFocus}
            dimNonRisk={dimNonRisk}
          />
        )}
      </SheetContent>
    </Sheet>
  );
}

function MetaRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-3 py-1.5 text-xs">
      <span className="text-muted-foreground">{label}</span>
      <span className="max-w-48 text-right font-medium break-words">
        {children}
      </span>
    </div>
  );
}

function SessionSummary({
  chat,
  messageCount,
  toolCount,
}: {
  chat: {
    externalUserId?: string;
    source?: string;
    createdAt: Date;
    totalCost?: number;
    totalInputTokens?: number;
    totalOutputTokens?: number;
    totalTokens?: number;
    lastMessageTimestamp?: Date;
    updatedAt: Date;
  };
  messageCount: number;
  toolCount: number;
}) {
  const tokens = totalTokensFor(chat);
  const hasCost = chat.totalCost !== undefined && chat.totalCost > 0;
  const endTime = chat.lastMessageTimestamp ?? chat.updatedAt;
  const duration = Math.round(
    (new Date(endTime).getTime() - new Date(chat.createdAt).getTime()) / 1000,
  );

  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          className="text-muted-foreground hover:text-foreground hover:bg-muted inline-flex items-center gap-2 rounded-md px-2 py-1 text-xs transition-colors"
        >
          {hasCost && (
            <span className="tabular-nums">
              {formatUsageCost(chat.totalCost!)}
            </span>
          )}
          {tokens > 0 && <span>{formatTokenCount(tokens)} tokens</span>}
          <Icon name="chevron-down" className="size-3" />
        </button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-72">
        <div className="space-y-1">
          <div className="mb-1 text-sm font-semibold">Session details</div>
          <div className="divide-border divide-y">
            <MetaRow label="User">{chat.externalUserId || "anonymous"}</MetaRow>
            {chat.source && (
              <MetaRow label="Source">
                <span className="inline-flex items-center gap-1.5">
                  <HookSourceIcon source={chat.source} className="size-3.5" />
                  {chat.source}
                </span>
              </MetaRow>
            )}
            <MetaRow label="Duration">{duration}s</MetaRow>
            <MetaRow label="Messages">{messageCount}</MetaRow>
            <MetaRow label="Tool calls">{toolCount}</MetaRow>
            <MetaRow label="Total cost">
              {hasCost ? formatUsageCost(chat.totalCost!) : "unknown"}
            </MetaRow>
            {chat.totalInputTokens !== undefined && (
              <MetaRow label="Input tokens">
                {chat.totalInputTokens.toLocaleString()}
              </MetaRow>
            )}
            {chat.totalOutputTokens !== undefined && (
              <MetaRow label="Output tokens">
                {chat.totalOutputTokens.toLocaleString()}
              </MetaRow>
            )}
            {tokens > 0 && (
              <MetaRow label="Total tokens">{tokens.toLocaleString()}</MetaRow>
            )}
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}

function ChatDetailHeader({
  chatId,
  chat,
  messageCount,
  toolCount,
  canManageChat,
  onExport,
  onDelete,
  onSetView,
  onClose,
}: {
  chatId: string;
  chat: Parameters<typeof SessionSummary>[0]["chat"] & { title?: string };
  messageCount: number;
  toolCount: number;
  canManageChat: boolean;
  onExport: () => void;
  onDelete: () => void;
  onSetView: (view: ViewMode) => void;
  onClose: () => void;
}) {
  return (
    <div className="border-b px-4 py-3">
      <div className="flex items-start justify-between gap-2">
        <div className="flex min-w-0 flex-col gap-1.5">
          <SheetTitle className="truncate text-base">
            {chat.title || getTraceId(chatId)}
          </SheetTitle>
          <SheetDescription asChild>
            <div className="flex items-center gap-2">
              <Badge
                variant="neutral"
                className="shrink-0 font-mono text-[10px]"
              >
                <Badge.Text>{getTraceId(chatId)}</Badge.Text>
              </Badge>
              <span className="text-muted-foreground shrink-0 font-mono text-xs">
                {format(new Date(chat.createdAt), "yyyy-MM-dd HH:mm")}
              </span>
            </div>
          </SheetDescription>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          <SessionSummary
            chat={chat}
            messageCount={messageCount}
            toolCount={toolCount}
          />
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <button
                type="button"
                className="text-muted-foreground hover:text-foreground hover:bg-muted inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-sm transition-colors"
              >
                <SlidersHorizontal className="size-4" />
                Actions
                <ChevronDown className="size-3.5" />
              </button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem
                className="cursor-pointer"
                onSelect={() => onSetView("tools")}
              >
                Tool calls{toolCount > 0 ? ` (${toolCount})` : ""}
              </DropdownMenuItem>
              {canManageChat && (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem
                    className="cursor-pointer"
                    onSelect={onExport}
                  >
                    Export data
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    className="text-destructive cursor-pointer"
                    onSelect={onDelete}
                  >
                    Delete session
                  </DropdownMenuItem>
                </>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
          <button
            onClick={onClose}
            className="hover:bg-muted rounded-md p-1 transition-colors"
            aria-label="Close panel"
          >
            <Icon name="x" className="size-5" />
          </button>
        </div>
      </div>
    </div>
  );
}

function SubViewBar({ title, onBack }: { title: string; onBack: () => void }) {
  return (
    <button
      type="button"
      onClick={onBack}
      className="text-muted-foreground hover:text-foreground hover:bg-muted/40 flex w-full items-center gap-2 border-b px-4 py-2 text-sm transition-colors"
    >
      <ArrowLeft className="size-4" />
      <span className="font-medium">Back to chat</span>
      <span className="text-muted-foreground/70">· {title}</span>
    </button>
  );
}

function ChatDetailPanel({
  chatId,
  onClose,
  onDelete,
  riskFocus = false,
  dimNonRisk: dimNonRiskProp = false,
}: ChatDetailPanelProps) {
  const isSuperAdmin = useIsAdmin();
  const { hasScope } = useRBAC();
  const canManageChat = isSuperAdmin || hasScope("org:admin");
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);
  const [view, setView] = useState<ViewMode>("chat");
  const [exclusionState, setExclusionState] =
    useState<ExclusionSheetState | null>(null);
  // The finding an in-flight exclusion was opened from, and the set of findings
  // hidden optimistically after their exclusion was created.
  const [pendingExclusionKey, setPendingExclusionKey] = useState<string | null>(
    null,
  );
  const [optimisticExcluded, setOptimisticExcluded] = useState<
    ReadonlySet<string>
  >(() => new Set());

  // Normal view paginates the latest generation by seq keyset; the risk view
  // loads server-windowed findings + context. Only the active one is enabled so
  // we don't double-fetch.
  const transcript = useChatTranscript(chatId, !riskFocus);
  const riskTranscript = useChatRiskTranscript(chatId, riskFocus);
  const active = riskFocus ? riskTranscript : transcript;
  const chat = active.chat;
  const chatMessages = active.messages;
  const chatLoading = active.isLoading;
  const chatLoadHasErrors = active.isError;

  const {
    mutate: searchLogs,
    data: logsData,
    isPending: logsLoading,
    error: logsError,
  } = useSearchLogsMutation();

  useEffect(() => {
    searchLogs({
      request: {
        searchLogsPayload: {
          filter: { gramChatId: chatId },
          limit: PANEL_TELEMETRY_LOG_LIMIT,
        },
      },
    });
  }, [chatId, searchLogs]);

  // Reset transient UI state when the panel is pointed at a new session.
  useEffect(() => {
    setView("chat");
    setExclusionState(null);
    setPendingExclusionKey(null);
    setOptimisticExcluded(new Set());
  }, [chatId]);

  const logs = useMemo(
    () => filterPanelTelemetryLogs(logsData?.logs ?? []),
    [logsData?.logs],
  );
  const toolLogs = useMemo(() => filterToolLogs(logs), [logs]);

  const { data: riskData } = useRiskListResults({ chatId });
  const riskResults = useMemo(() => {
    const all = riskData?.results ?? [];
    if (optimisticExcluded.size === 0) return all;
    return all.filter((r) => !optimisticExcluded.has(findingKey(r)));
  }, [riskData?.results, optimisticExcluded]);
  const riskResultsByMessage = useMemo(() => {
    const map = new Map<string, RiskResult[]>();
    for (const r of riskResults) {
      const existing = map.get(r.chatMessageId);
      if (existing) existing.push(r);
      else map.set(r.chatMessageId, [r]);
    }
    return map;
  }, [riskResults]);

  const claudeUsageByMessage = useMemo(() => {
    const turns =
      chat?.agentUsage?.type === "claude"
        ? (chat.agentUsage.claude?.turns ?? [])
        : [];
    return buildClaudeUsageByMessageId({ messages: chatMessages, turns });
  }, [chat?.agentUsage, chatMessages]);
  const claudeToolUsageByToolUseId = useMemo(() => {
    const tools =
      chat?.agentUsage?.type === "claude"
        ? (chat.agentUsage.claude?.tools ?? [])
        : [];
    return buildClaudeToolUsageByToolUseId(tools);
  }, [chat?.agentUsage]);

  const transcriptRows = useMemo(
    () => buildTranscript(chatMessages),
    [chatMessages],
  );
  const hasMoreBefore = active.hasMoreBefore;
  const hasMoreAfter = active.hasMoreAfter;
  const riskGaps = riskFocus ? riskTranscript.gaps : undefined;
  const displayItems = useMemo(
    () =>
      buildDisplayItems({
        rows: transcriptRows,
        hasMoreBefore,
        hasMoreAfter,
        gaps: riskGaps,
      }),
    [transcriptRows, hasMoreBefore, hasMoreAfter, riskGaps],
  );

  const transcriptPagination = useMemo<TranscriptPagination>(
    () => ({
      hasMoreBefore,
      hasMoreAfter,
      onLoadOlder: () =>
        riskFocus ? riskTranscript.loadBefore() : transcript.fetchOlder(),
      onLoadNewer: () =>
        riskFocus ? riskTranscript.loadAfter() : transcript.fetchNewer(),
      isFetchingOlder: riskFocus
        ? riskTranscript.loadingKey === "before"
        : transcript.isFetchingOlder,
      isFetchingNewer: riskFocus
        ? riskTranscript.loadingKey === "after"
        : transcript.isFetchingNewer,
      onLoadGap: riskFocus ? riskTranscript.loadGap : undefined,
      isLoadingGap: riskFocus
        ? (afterSeq: number) => riskTranscript.loadingKey === `gap:${afterSeq}`
        : undefined,
      // Normal view opens at the newest message; risk view starts at the top.
      initialScrollToEnd: !riskFocus,
    }),
    [hasMoreBefore, hasMoreAfter, riskFocus, riskTranscript, transcript],
  );

  // Spotlight findings in the risk-focused view or when opened from a
  // risk-filtered list (e.g. Agent Sessions has_risk). NOT in plain cost views.
  const dimNonRisk = riskFocus || dimNonRiskProp;
  const rowCtx = useMemo<RowContext>(
    () => ({
      riskResultsByMessage,
      claudeUsageByMessage,
      claudeToolUsageByToolUseId,
      dimNonRisk,
      riskFocus,
    }),
    [
      riskResultsByMessage,
      claudeUsageByMessage,
      claudeToolUsageByToolUseId,
      dimNonRisk,
      riskFocus,
    ],
  );

  // "Create exclusion" swaps the transcript for the exclusion editor in-place
  // (with a back button) rather than stacking a second sheet on top.
  const openExclusion = useCallback((result: RiskResult) => {
    setExclusionState(findingToExclusionState(result));
    setPendingExclusionKey(findingKey(result));
    setView("exclusion");
  }, []);
  const closeExclusion = useCallback(() => {
    setView("chat");
    setExclusionState(null);
    setPendingExclusionKey(null);
  }, []);
  const handleExclusionDone = useCallback(() => {
    setOptimisticExcluded((prev) => {
      if (!pendingExclusionKey) return prev;
      const next = new Set(prev);
      next.add(pendingExclusionKey);
      return next;
    });
    closeExclusion();
  }, [pendingExclusionKey, closeExclusion]);

  if (chatLoading) {
    return (
      <div className="p-8">
        <SheetTitle>Loading</SheetTitle>
        <SheetDescription>Fetching chat session details...</SheetDescription>
      </div>
    );
  }

  if (!chat) {
    return (
      <div className="p-8">
        <SheetTitle>Not found</SheetTitle>
        <SheetDescription>
          The selected chat session could not be found.
        </SheetDescription>
      </div>
    );
  }

  const error = logsError as Error | null;

  return (
    <div className="bg-background flex h-full flex-col">
      <ChatDetailHeader
        chatId={chatId}
        chat={chat}
        messageCount={chatMessages.length}
        toolCount={toolLogs.length}
        canManageChat={canManageChat}
        onExport={() => {
          exportTraceDataAsJson({
            chatId,
            chat,
            messages: chatMessages,
            telemetryLogLimit: PANEL_TELEMETRY_LOG_LIMIT,
            telemetryLogs: logs,
            riskResults,
          });
        }}
        onDelete={() => setShowDeleteConfirm(true)}
        onSetView={setView}
        onClose={onClose}
      />

      {chatLoadHasErrors && (
        <div className="border-destructive/30 bg-destructive/10 text-destructive border-b px-4 py-2 text-xs">
          Some messages failed to load. The transcript below may be incomplete.
        </div>
      )}

      {view === "chat" && (
        <CreateExclusionContext.Provider
          value={canManageChat ? openExclusion : null}
        >
          <ChatTranscript
            items={displayItems}
            ctx={rowCtx}
            pagination={transcriptPagination}
            emptyMessage={
              riskFocus
                ? "No flagged messages in this session."
                : "No messages to display."
            }
          />
        </CreateExclusionContext.Provider>
      )}

      {view === "logs" && (
        <>
          <SubViewBar title="Telemetry logs" onBack={() => setView("chat")} />
          <div className="flex-1 overflow-y-auto">
            <TelemetryLogsView
              logs={logs}
              isLoading={logsLoading}
              error={error}
            />
          </div>
        </>
      )}

      {view === "tools" && (
        <>
          <SubViewBar title="Tool calls" onBack={() => setView("chat")} />
          <div className="flex-1 overflow-y-auto">
            <ToolCallsView
              toolLogs={toolLogs}
              isLoading={logsLoading}
              error={error}
            />
          </div>
        </>
      )}

      {view === "exclusion" && exclusionState && (
        <div className="flex flex-1 flex-col overflow-hidden">
          <SubViewBar
            title={
              exclusionState.mode === "edit"
                ? "Edit exclusion"
                : "Create exclusion"
            }
            onBack={closeExclusion}
          />
          <p className="text-muted-foreground px-6 pt-4 text-sm">
            Suppress matching findings retroactively and going forward. Does not
            re-run analysis.
          </p>
          <ExclusionEditor
            state={exclusionState}
            onDone={handleExclusionDone}
          />
        </div>
      )}

      <Dialog open={showDeleteConfirm} onOpenChange={setShowDeleteConfirm}>
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Delete chat session</Dialog.Title>
            <Dialog.Description>
              Are you sure you want to delete this chat session? This action
              cannot be undone.
            </Dialog.Description>
          </Dialog.Header>
          <Dialog.Footer>
            <Dialog.Close asChild>
              <Button variant="secondary">Cancel</Button>
            </Dialog.Close>
            <Button
              variant="destructive-primary"
              onClick={() => {
                onDelete(chatId);
                setShowDeleteConfirm(false);
              }}
            >
              Delete
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </div>
  );
}
