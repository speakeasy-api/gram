import { IdentityLink } from "@/components/identity-link";
import { format, formatDistanceToNow } from "date-fns";
import {
  ArrowLeft,
  ChevronDown,
  ChevronsDown,
  ChevronUp,
  GitBranch,
  Info,
  Loader2,
  Pin,
  Search,
  SlidersHorizontal,
  Sparkles,
  TriangleAlert,
  X,
} from "lucide-react";
import {
  type ReactNode,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { Icon } from "@/components/ui/Icon";
import type { ChatOverview } from "@gram/client/models/components/chatoverview.js";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useSearchLogsMutation } from "@gram/client/react-query/searchLogs.js";
import { useRiskListResults } from "@gram/client/react-query/riskListResults.js";
import { useChatSetPinnedMutation } from "@gram/client/react-query/chatSetPinned.js";
import { invalidateAllListChats } from "@gram/client/react-query/listChats.js";
import { useListChatSessionLinks } from "@gram/client/react-query/listChatSessionLinks.js";
import { useSummarizeChatMutation } from "@gram/client/react-query/summarizeChat.js";
import { QueryErrorResetBoundary, useQueryClient } from "@tanstack/react-query";
import { ErrorBoundary, type FallbackProps } from "react-error-boundary";
import { toast } from "sonner";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetTitle,
} from "@/components/ui/Sheet";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";
import { Dialog } from "@/components/ui/Dialog";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { Switch } from "@/components/ui/Switch";
import { AccountTypeBadge } from "@/components/account-type-badge";
import { personalAccountEmail } from "@/components/observe/account-display-utils";
import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import { useRBAC } from "@/hooks/useRBAC";
import { useIsPlatformAdmin, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { ChatOwnerLabel } from "@/components/chat-owner-label";
import { chatOwnerLabel } from "@/lib/chat-owner";
import { handleError, toError } from "@/lib/errors";
import { formatChatSource, formatPlatform } from "@/lib/formatPlatform";
import {
  ExclusionEditor,
  type ExclusionSheetState,
} from "@/pages/security/exclusion-sheet";
import { useChatTranscript } from "./useChatTranscript";
import { useWindowedTranscript } from "./useWindowedTranscript";
import { CreateExclusionContext } from "./exclusionContext";
import { riskResultAnchorId } from "./chatHelpers";
import {
  ChatTranscript,
  type RowContext,
  type TranscriptPagination,
} from "./ChatTranscript";
import {
  buildDisplayItems,
  buildTranscript,
  displayItemContainsMessage,
  displayItemRows,
  rowHasRiskFlag,
  rowIsFlagged,
  rowSearchFields,
  type SearchFieldKey,
} from "./transcript";
import { cn } from "@/lib/utils";
import {
  buildClaudeToolUsageByToolUseId,
  buildClaudeTurnByPromptId,
  buildClaudeUsageByMessageId,
  formatTokenCount,
  formatUsageCost,
} from "./claudeUsage";
import { filterPanelTelemetryLogs, filterToolLogs } from "./chatLogFilters";
import { WorkUnitsHeaderMetrics } from "./WorkUnitsMetrics";
import { formatWorkUnits, workUnitsEfficiency } from "./workUnits";
import { ToolCallsView } from "./chatLogViews";
import { EffectsView } from "./EffectsView";
import { exportTraceDataAsJson } from "./chatExport";

const PANEL_TELEMETRY_LOG_LIMIT = 100;

// Mirrors the server-side `MaxLength(200)` on chat.load's `query` param (see
// server/design/chat/design.go). Queries longer than this are rejected with a
// hard 400, so we gate the request and flag the input instead of firing it.
const MAX_SEARCH_QUERY_LEN = 200;

interface ChatDetailPanelProps {
  chatId: string;
  onClose: () => void;
  onDelete: (chatId: string) => void;
  /** One-based raw transcript message index to load, center, and highlight. */
  focusedMessageTurn?: number;
  /** Risk-focused view: collapse the transcript to the flagged messages plus a
   * few of context either side, expandable via "show more". Implies dimming. */
  riskFocus?: boolean;
  /** Dim non-flagged rows to spotlight findings, without the risk windowing.
   * Use from risk-filtered lists (e.g. Agent Sessions filtered to has_risk). */
  dimNonRisk?: boolean;
  /** Open a different chat in the panel — used by the Linked-sessions section
   * to hop to a move's other end. Omitted, linked sessions render unlinked. */
  onOpenChat?: (chatId: string) => void;
}

interface ChatDetailSheetProps extends Omit<ChatDetailPanelProps, "chatId"> {
  chatId: string | null;
}

type ViewMode = "chat" | "effects" | "tools" | "exclusion";

/** One navigable search hit: a single query occurrence within one field of one
 * display row. `key` is stable across pagination (built from the row id, not its
 * shifting index) so the active occurrence survives loading more messages. */
interface Occurrence {
  key: string;
  itemIndex: number;
  /** Row holding the occurrence. A `toolGroup` item covers many rows, so the
   * item index alone can't say which one to light up. */
  rowId: string;
  fieldKey: SearchFieldKey;
  indexInField: number;
}

// Stable empty array for the no-search case, so memo/effect deps that read the
// occurrence list don't see a fresh identity every render.
const EMPTY_OCCURRENCES: Occurrence[] = [];

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

// ChatDetailErrorFallback is the defensive backstop for unexpected
// render-time throws inside the sheet (anything beyond the anticipated
// not-found/forbidden states the panel already handles inline) — it keeps a
// crash scoped to the sheet's content instead of tripping the page-wide
// ContentErrorBoundary and wiping the rest of Risk Events / Chat Logs behind
// it. Retry resets the boundary and any errored queries in place.
function ChatDetailErrorFallback({
  error: rawError,
  resetErrorBoundary,
}: FallbackProps): JSX.Element {
  const error = toError(rawError);
  handleError(error, { silent: true });

  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 p-8 text-center">
      <Icon name="circle-alert" className="text-destructive h-6 w-6" />
      <div>
        <p className="font-medium">Something went wrong loading this chat.</p>
        <p className="text-muted-foreground mt-1 text-sm">{error.message}</p>
      </div>
      <Button variant="secondary" size="sm" onClick={resetErrorBoundary}>
        <Button.LeftIcon>
          <Icon name="rotate-ccw" className="h-4 w-4" />
        </Button.LeftIcon>
        <Button.Text>Retry</Button.Text>
      </Button>
    </div>
  );
}

export function ChatDetailSheet({
  chatId,
  onClose,
  onDelete,
  focusedMessageTurn,
  riskFocus,
  dimNonRisk,
  onOpenChat,
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
          <QueryErrorResetBoundary>
            {({ reset }) => (
              <ErrorBoundary
                onReset={reset}
                FallbackComponent={ChatDetailErrorFallback}
              >
                <ChatDetailPanel
                  chatId={chatId}
                  onClose={onClose}
                  onDelete={onDelete}
                  focusedMessageTurn={focusedMessageTurn}
                  riskFocus={riskFocus}
                  dimNonRisk={dimNonRisk}
                  onOpenChat={onOpenChat}
                />
              </ErrorBoundary>
            )}
          </QueryErrorResetBoundary>
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
  userLabel,
  messageCount,
  toolCount,
  compact = false,
}: {
  chat: {
    externalUserId?: string;
    accountType?: string;
    accountEmail?: string;
    source?: string;
    originatingClient?: string;
    litellmProxied?: boolean;
    createdAt: Date;
    totalCost?: number;
    totalInputTokens?: number;
    totalOutputTokens?: number;
    totalTokens?: number;
    lastMessageTimestamp?: Date;
    updatedAt: Date;
    workUnits?: number;
    workUnitsReport?: string;
  };
  userLabel: ReactNode;
  messageCount: number;
  toolCount: number;
  compact?: boolean;
}) {
  const tokens = totalTokensFor(chat);
  const hasCost = chat.totalCost !== undefined && chat.totalCost > 0;
  const {
    costPerUnit: workUnitsCostPerUnit,
    tokensPerUnit: workUnitsTokensPerUnit,
  } = workUnitsEfficiency(chat);
  const accountEmail = personalAccountEmail(chat);
  const endTime = chat.lastMessageTimestamp ?? chat.updatedAt;
  const duration = Math.round(
    (new Date(endTime).getTime() - new Date(chat.createdAt).getTime()) / 1000,
  );

  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          className="text-muted-foreground hover:text-foreground hover:bg-muted inline-flex items-center gap-1.5 px-2 py-1 text-sm transition-colors"
        >
          {compact ? (
            <span className="inline-flex items-center gap-1.5">
              <Info className="size-3.5" />
              Details
            </span>
          ) : (
            <>
              {hasCost && (
                <span className="tabular-nums">
                  {formatUsageCost(chat.totalCost!)}
                </span>
              )}
              {hasCost && tokens > 0 && (
                <span aria-hidden className="text-muted-foreground/40">
                  |
                </span>
              )}
              {tokens > 0 && <span>{formatTokenCount(tokens)} tokens</span>}
            </>
          )}
          {/* No cost telemetry for this session (e.g. ClickHouse miss) — show a
              neutral "Details" label so the trigger is never an empty pill. */}
          {!compact && !hasCost && tokens === 0 && (
            <span className="inline-flex items-center gap-1.5">
              <Info className="size-3.5" />
              Details
            </span>
          )}
          <ChevronDown className="size-3.5" />
        </button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-72">
        <div className="space-y-1">
          <div className="mb-1 text-sm font-semibold">Session details</div>
          <div className="divide-border divide-y">
            <MetaRow label="User">{userLabel}</MetaRow>
            {accountEmail && (
              <MetaRow label="Account">
                <span className="inline-flex flex-wrap items-center justify-end gap-1.5">
                  <IdentityLink identifier={{ email: accountEmail }}>
                    {accountEmail}
                  </IdentityLink>
                  <AccountTypeBadge accountType={chat.accountType} noTooltip />
                </span>
              </MetaRow>
            )}
            {chat.source && (
              <MetaRow label="Source">
                <span className="inline-flex items-center gap-1.5">
                  <AgentProviderIcon
                    source={chat.source}
                    className="size-3.5"
                  />
                  {formatChatSource(chat.source, chat)}
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
            {chat.workUnits !== undefined && (
              <MetaRow label="Work delivered">
                {formatWorkUnits(chat.workUnits)}
              </MetaRow>
            )}
            {workUnitsCostPerUnit !== null && (
              <MetaRow label="Cost efficiency">
                {formatUsageCost(workUnitsCostPerUnit)}
              </MetaRow>
            )}
            {workUnitsTokensPerUnit !== null && (
              <MetaRow label="Token efficiency">
                {formatTokenCount(workUnitsTokensPerUnit)}
              </MetaRow>
            )}
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}

function HeaderMetadataBadge({ children }: { children: ReactNode }) {
  return (
    <Badge variant="neutral" className="shrink-0 font-mono text-[10px]">
      <Badge.Text>{children}</Badge.Text>
    </Badge>
  );
}

function ChatDetailMetadataBadges({
  chatId,
  chat,
  messageCount,
  toolCount,
}: {
  chatId: string;
  chat: Parameters<typeof SessionSummary>[0]["chat"];
  messageCount: number;
  toolCount: number;
}) {
  const tokens = totalTokensFor(chat);
  const hasCost = chat.totalCost !== undefined && chat.totalCost > 0;

  return (
    <div className="flex flex-wrap items-center gap-2">
      <HeaderMetadataBadge>{getTraceId(chatId)}</HeaderMetadataBadge>
      <HeaderMetadataBadge>
        {messageCount.toLocaleString()} messages
      </HeaderMetadataBadge>
      {toolCount > 0 && (
        <HeaderMetadataBadge>
          {toolCount.toLocaleString()} tool calls
        </HeaderMetadataBadge>
      )}
      {chat.source && (
        <Badge variant="neutral" className="shrink-0 text-[10px]">
          <Badge.Text>
            <span className="inline-flex items-center gap-1.5">
              <AgentProviderIcon source={chat.source} className="size-3" />
              {formatChatSource(chat.source, chat)}
            </span>
          </Badge.Text>
        </Badge>
      )}
      {hasCost && (
        <HeaderMetadataBadge>
          {formatUsageCost(chat.totalCost!)}
        </HeaderMetadataBadge>
      )}
      {tokens > 0 && (
        <HeaderMetadataBadge>
          {formatTokenCount(tokens)} tokens
        </HeaderMetadataBadge>
      )}
    </div>
  );
}

/** Header filter bar: condensed-view toggle + optional "Risky only" switch. */
function MessageFilterBar({
  condensed,
  onCondensedChange,
  riskyOnly,
  onRiskyOnlyChange,
  showRiskyOnly,
}: {
  condensed: boolean;
  onCondensedChange: (next: boolean) => void;
  riskyOnly: boolean;
  onRiskyOnlyChange: (next: boolean) => void;
  showRiskyOnly: boolean;
}) {
  return (
    <div className="flex items-center justify-end gap-3">
      <div className="flex items-center gap-2">
        <Switch
          checked={condensed}
          onCheckedChange={onCondensedChange}
          aria-label="Condensed view"
        />
        <span className="text-muted-foreground text-xs font-medium">
          Condensed
        </span>
      </div>
      {showRiskyOnly && (
        <>
          <div className="bg-border h-5 w-px" />
          <div className="flex items-center gap-2">
            <Switch
              checked={riskyOnly}
              onCheckedChange={onRiskyOnlyChange}
              aria-label="Show only risky messages"
              className={riskyOnly ? "bg-destructive" : undefined}
            />
            <span className="text-muted-foreground text-xs font-medium">
              Risky only
            </span>
          </div>
        </>
      )}
    </div>
  );
}

/** Find-in-conversation bar: a text box plus a match counter and prev/next
 * navigation. Server-backed full-thread search; the panel debounces the input
 * and drives the jump-to-match scrolling. */
function ThreadSearchBar({
  value,
  onChange,
  matchCount,
  activeIndex,
  loading,
  onPrev,
  onNext,
}: {
  value: string;
  onChange: (next: string) => void;
  matchCount: number;
  activeIndex: number;
  loading: boolean;
  onPrev: () => void;
  onNext: () => void;
}) {
  const trimmedLen = value.trim().length;
  const hasQuery = trimmedLen > 0;
  // Over the server's query cap: don't pretend to search — show a red counter in
  // the match-count slot so the user sees why nothing is happening, and hide the
  // (meaningless) prev/next nav while keeping clear available.
  const overLimit = trimmedLen > MAX_SEARCH_QUERY_LEN;
  const navBtn =
    "text-muted-foreground hover:text-foreground hover:bg-background flex size-6 shrink-0 items-center justify-center transition-colors disabled:opacity-40";
  return (
    <div className="bg-background focus-within:border-foreground/40 flex h-9 items-center gap-2 border px-2.5 transition-colors">
      {overLimit ? (
        <SimpleTooltip
          tooltip={`Queries are limited to ${MAX_SEARCH_QUERY_LEN} characters`}
        >
          <TriangleAlert className="text-destructive size-3.5 shrink-0" />
        </SimpleTooltip>
      ) : (
        <Search className="text-muted-foreground size-3.5 shrink-0" />
      )}
      <input
        type="text"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => {
          // Don't hijack keys mid-IME-composition (e.g. selecting a CJK
          // candidate with Enter) — let the input method consume them.
          if (e.nativeEvent.isComposing) return;
          // Enter jumps to the next match; Shift+Enter the previous. Both wrap.
          if (e.key === "Enter") {
            e.preventDefault();
            if (e.shiftKey) onPrev();
            else onNext();
          }
          // Escape clears the query (and is swallowed) while one is typed, so it
          // doesn't bubble up and close the whole sheet.
          if (e.key === "Escape" && value.trim().length > 0) {
            e.preventDefault();
            e.stopPropagation();
            onChange("");
          }
        }}
        placeholder="Search this conversation…"
        className="placeholder:text-muted-foreground/70 min-w-0 flex-1 bg-transparent text-xs outline-none"
      />
      {hasQuery && (
        <>
          {overLimit ? (
            <span className="text-destructive shrink-0 text-xs tabular-nums">
              {trimmedLen}/{MAX_SEARCH_QUERY_LEN}
            </span>
          ) : (
            <span className="text-muted-foreground shrink-0 text-xs tabular-nums">
              {loading
                ? "…"
                : matchCount > 0
                  ? `${activeIndex + 1}/${matchCount}`
                  : "0/0"}
            </span>
          )}
          {!overLimit && (
            <>
              <button
                type="button"
                onClick={onPrev}
                disabled={matchCount === 0}
                aria-label="Previous match"
                className={navBtn}
              >
                <ChevronUp className="size-3.5" />
              </button>
              <button
                type="button"
                onClick={onNext}
                disabled={matchCount === 0}
                aria-label="Next match"
                className={navBtn}
              >
                <ChevronDown className="size-3.5" />
              </button>
            </>
          )}
          <button
            type="button"
            onClick={() => onChange("")}
            aria-label="Clear search"
            className={navBtn}
          >
            <X className="size-3.5" />
          </button>
        </>
      )}
    </div>
  );
}

function ChatDetailHeader({
  chatId,
  chat,
  userLabel,
  messageCount,
  toolCount,
  canManageChat,
  compactMetadata,
  showFilter,
  condensed,
  onCondensedChange,
  riskyOnly,
  onRiskyOnlyChange,
  showRiskyOnly,
  searchBar,
  pinned,
  onTogglePinned,
  pinPending,
  onExport,
  onDelete,
  onSetView,
  onClose,
}: {
  chatId: string;
  chat: Parameters<typeof SessionSummary>[0]["chat"] & { title?: string };
  userLabel: ReactNode;
  messageCount: number;
  toolCount: number;
  canManageChat: boolean;
  compactMetadata: boolean;
  showFilter: boolean;
  condensed: boolean;
  onCondensedChange: (next: boolean) => void;
  riskyOnly: boolean;
  onRiskyOnlyChange: (next: boolean) => void;
  showRiskyOnly: boolean;
  /** Optional find-in-conversation bar (normal view only). */
  searchBar?: ReactNode;
  pinned: boolean;
  onTogglePinned: () => void;
  pinPending: boolean;
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
            <div className="flex flex-col items-start gap-1.5">
              <span className="text-muted-foreground text-sm">
                {formatDistanceToNow(new Date(chat.createdAt), {
                  addSuffix: true,
                })}{" "}
                <span className="font-mono">
                  ({format(new Date(chat.createdAt), "yyyy-MM-dd HH:mm")})
                </span>
              </span>
              {compactMetadata ? (
                <ChatDetailMetadataBadges
                  chatId={chatId}
                  chat={chat}
                  messageCount={messageCount}
                  toolCount={toolCount}
                />
              ) : (
                <HeaderMetadataBadge>{getTraceId(chatId)}</HeaderMetadataBadge>
              )}
              <WorkUnitsHeaderMetrics chat={chat} />
            </div>
          </SheetDescription>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          <SessionSummary
            chat={chat}
            userLabel={userLabel}
            messageCount={messageCount}
            toolCount={toolCount}
            compact={compactMetadata}
          />
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <button
                type="button"
                className="text-muted-foreground hover:text-foreground hover:bg-muted inline-flex items-center gap-1.5 px-2 py-1 text-sm transition-colors"
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
              <DropdownMenuItem
                className="cursor-pointer gap-2"
                disabled={pinPending}
                onSelect={onTogglePinned}
              >
                <Pin
                  className={cn("size-3.5", pinned && "fill-current")}
                  aria-hidden
                />
                {pinned ? "Unpin session" : "Pin session"}
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
            className="hover:bg-muted p-1 transition-colors"
            aria-label="Close panel"
          >
            <Icon name="x" className="size-5" />
          </button>
        </div>
      </div>
      {showFilter && (
        <div className="mt-3 flex items-center gap-3">
          {/* Search (when present) flexes to fill the left; an empty spacer keeps
              the filters right-aligned in the risk view where search is hidden. */}
          <div className="min-w-0 flex-1">{searchBar}</div>
          <div className="shrink-0">
            <MessageFilterBar
              condensed={condensed}
              onCondensedChange={onCondensedChange}
              riskyOnly={riskyOnly}
              onRiskyOnlyChange={onRiskyOnlyChange}
              showRiskyOnly={showRiskyOnly}
            />
          </div>
        </div>
      )}
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

// SessionLinksSection lists session-lineage edges touching this chat: moves
// and recalls out of it ("Moved to …", "Recalled into …") and moves that
// produced it ("Derived from …").
// Presence-gated — chats with no edges render nothing, so there is no feature
// flag and no empty state.
function SessionLinksSection({
  chatId,
  onOpenChat,
}: {
  chatId: string;
  onOpenChat?: (chatId: string) => void;
}) {
  // throwOnError stays off: this section is optional, and a lineage-endpoint
  // failure must not take down the whole transcript panel's error boundary.
  const { data } = useListChatSessionLinks({ chatIds: [chatId] }, undefined, {
    throwOnError: false,
  });
  const links = data?.links ?? [];
  if (links.length === 0) {
    return null;
  }

  const outbound = links.filter((l) => l.parentChatId === chatId);
  const inbound = links.filter((l) => l.childChatId === chatId);

  const hop = (target: string | undefined, navigable: boolean) =>
    onOpenChat && navigable && target ? () => onOpenChat(target) : undefined;

  const row = (
    key: string,
    label: ReactNode,
    when: Date,
    onHop: (() => void) | undefined,
    detail?: ReactNode,
  ) => (
    <div key={key} className="flex items-baseline justify-between gap-3 py-1">
      <span className="min-w-0 truncate text-xs">
        {onHop ? (
          <button
            type="button"
            onClick={onHop}
            className="text-foreground cursor-pointer font-medium underline-offset-2 hover:underline"
          >
            {label}
          </button>
        ) : (
          <span className="text-foreground font-medium">{label}</span>
        )}
        {detail && <span className="text-muted-foreground"> · {detail}</span>}
      </span>
      <span
        className="text-muted-foreground shrink-0 text-xs"
        title={format(when, "PPpp")}
      >
        {formatDistanceToNow(when, { addSuffix: true })}
      </span>
    </div>
  );

  return (
    <div className="border-b px-4 py-3">
      <div className="text-foreground inline-flex items-center gap-1.5 text-sm font-medium">
        <GitBranch className="size-3.5" aria-hidden />
        Linked sessions
      </div>
      <div className="mt-1">
        {inbound.map((link, i) =>
          row(
            `in-${i}-${link.createdAt.toISOString()}`,
            <>Derived from {link.parentTitle ?? "an earlier session"}</>,
            link.createdAt,
            hop(link.parentChatId, link.parentCaptured),
            link.parentCaptured ? (
              link.actorEmail ? (
                <IdentityLink identifier={{ email: link.actorEmail }}>
                  {link.actorEmail}
                </IdentityLink>
              ) : undefined
            ) : (
              "not yet captured"
            ),
          ),
        )}
        {outbound.map((link, i) =>
          // Recall edges never carry a child chat (the continuation is
          // unknowable at recall time), so there is nothing to hop to and
          // "not yet captured" would wrongly imply one is expected.
          link.kind === "recall"
            ? row(
                `out-${i}-${link.createdAt.toISOString()}`,
                <>Recalled into a later session</>,
                link.createdAt,
                undefined,
              )
            : row(
                `out-${i}-${link.createdAt.toISOString()}`,
                <>Moved to {formatPlatform(link.targetHarness)}</>,
                link.createdAt,
                hop(link.childChatId, link.childCaptured),
                link.childCaptured
                  ? (link.childTitle ?? undefined)
                  : "not yet captured",
              ),
        )}
      </div>
    </div>
  );
}

function SessionSummarySection({
  chatId,
  summary,
  summaryGeneratedAt,
  onSummaryChange,
}: {
  chatId: string;
  summary?: string;
  summaryGeneratedAt?: Date;
  onSummaryChange: (
    summary: string,
    generatedAt: Date,
    forChatId: string,
  ) => void;
}) {
  const queryClient = useQueryClient();
  const summarize = useSummarizeChatMutation();
  const [expanded, setExpanded] = useState(true);
  const hasSummary = Boolean(summary?.trim());
  // Track the active session so a late summarize response cannot apply to a
  // different chat after the panel navigates away.
  const activeChatIdRef = useRef(chatId);
  activeChatIdRef.current = chatId;

  const runSummarize = (regenerate: boolean) => {
    const requestedChatId = chatId;
    summarize.mutate(
      {
        request: {
          summarizeRequestBody: { id: requestedChatId, regenerate },
        },
      },
      {
        onSuccess: (result) => {
          if (activeChatIdRef.current !== requestedChatId) {
            return;
          }
          onSummaryChange(
            result.summary,
            result.summaryGeneratedAt,
            requestedChatId,
          );
          void queryClient.invalidateQueries({
            queryKey: ["chat", requestedChatId, "transcript"],
          });
          void invalidateAllListChats(queryClient);
        },
        onError: (error) => {
          if (activeChatIdRef.current !== requestedChatId) {
            return;
          }
          toast.error(error.message || "Failed to summarize session");
        },
      },
    );
  };

  return (
    <div className="border-b px-4 py-3">
      <div className="flex items-center justify-between gap-2">
        <button
          type="button"
          onClick={() => setExpanded((prev) => !prev)}
          className="text-foreground inline-flex items-center gap-1.5 text-sm font-medium"
        >
          <Sparkles className="size-3.5" aria-hidden />
          Summary
          {expanded ? (
            <ChevronUp className="text-muted-foreground size-3.5" />
          ) : (
            <ChevronDown className="text-muted-foreground size-3.5" />
          )}
        </button>
        <Button
          variant="secondary"
          size="sm"
          disabled={summarize.isPending}
          onClick={() => runSummarize(hasSummary)}
        >
          <Button.LeftIcon>
            {summarize.isPending ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : (
              <Sparkles className="size-3.5" />
            )}
          </Button.LeftIcon>
          <Button.Text>
            {summarize.isPending
              ? "Summarizing…"
              : hasSummary
                ? "Regenerate"
                : "Summarize"}
          </Button.Text>
        </Button>
      </div>
      {expanded && (
        <div className="mt-2">
          {hasSummary ? (
            <>
              <p className="text-foreground/90 text-sm leading-relaxed whitespace-pre-wrap">
                {summary}
              </p>
              {summaryGeneratedAt && (
                <p className="text-muted-foreground mt-2 text-xs">
                  Generated{" "}
                  {formatDistanceToNow(summaryGeneratedAt, { addSuffix: true })}
                </p>
              )}
            </>
          ) : (
            <p className="text-muted-foreground text-sm">
              No summary yet. Generate one to get a concise overview of this
              session.
            </p>
          )}
        </div>
      )}
    </div>
  );
}

function ChatDetailPanel({
  chatId,
  onClose,
  onDelete,
  focusedMessageTurn,
  riskFocus = false,
  dimNonRisk: dimNonRiskProp = false,
  onOpenChat,
}: ChatDetailPanelProps) {
  const client = useSdkClient();
  const { user } = useSession();
  const isPlatformAdmin = useIsPlatformAdmin();
  const { hasScope } = useRBAC();
  const canManageChat = isPlatformAdmin || hasScope("org:admin");
  // Risk findings are an org-admin resource (risk.results.list is org-admin
  // gated). Only admins get the risk-windowed "Risky only" view + its data.
  const canViewRisk = isPlatformAdmin || hasScope("org:admin");
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);
  const [view, setView] = useState<ViewMode>("chat");
  const [riskyOnly, setRiskyOnly] = useState(false);
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
  // Find-in-conversation: the raw input, its debounced value (which drives the
  // search request + its react-query key), the active occurrence (tracked by a
  // stable key so paging in more rows doesn't shift which occurrence is current),
  // and a nonce bumped on each jump so re-pressing next re-scrolls.
  const [searchInput, setSearchInput] = useState("");
  const [searchQuery, setSearchQuery] = useState("");
  const [activeOccurrenceKey, setActiveOccurrenceKey] = useState<string | null>(
    null,
  );
  const [scrollNonce, setScrollNonce] = useState(0);
  const [localSummary, setLocalSummary] = useState<string | undefined>();
  const [localSummaryGeneratedAt, setLocalSummaryGeneratedAt] = useState<
    Date | undefined
  >();
  const queryClient = useQueryClient();
  const setPinnedMutation = useChatSetPinnedMutation();
  useEffect(() => {
    const handle = setTimeout(() => setSearchQuery(searchInput.trim()), 250);
    return () => clearTimeout(handle);
  }, [searchInput]);
  useEffect(() => {
    setLocalSummary(undefined);
    setLocalSummaryGeneratedAt(undefined);
  }, [chatId]);

  // Risk-review contexts — explicit risk focus, or opened from the has-risk
  // filter — load the server-windowed risk transcript so findings load no matter
  // which page they sit on, and spotlight them. Plain views paginate the latest
  // generation by seq keyset. Only the active transcript is enabled so we don't
  // double-fetch.
  const dimNonRisk = riskFocus || dimNonRiskProp;
  // The risk-windowed view loads via the server `risk_only` param so every
  // finding is fetched no matter which page it sits on. Two entry points turn it
  // on: opening from the has-risk filter / risk focus (`dimNonRisk`, which dims
  // non-flagged context), and the in-panel "Risky only" toggle (`riskyOnly`,
  // which hard-filters down to just the flagged rows in `visibleRows` below). A
  // plain client-side filter can't do this — it only sees the loaded page.
  const riskWindowed = (dimNonRisk || riskyOnly) && canViewRisk;
  // Always load the latest page for chat-level enrichment (cost, agent usage,
  // source) — the risk-only load omits it. Risk views additionally load the
  // server-windowed findings and render those instead of the latest page.
  const transcript = useChatTranscript(chatId, true);
  const riskTranscript = useWindowedTranscript(chatId, riskWindowed, {
    riskOnly: true,
  });
  // Search is a third windowed mode, available only in the normal (non-risk,
  // non-risky-only) view. Disabled (no fetch) until there's a debounced query.
  const searchActive =
    !riskWindowed &&
    searchQuery.length > 0 &&
    searchQuery.length <= MAX_SEARCH_QUERY_LEN;
  const searchTranscript = useWindowedTranscript(chatId, searchActive, {
    query: searchQuery,
  });
  // Risk and search are both server-windowed; only their source differs. The
  // plain keyset transcript drives loads directly when neither is active.
  const windowedChoice = riskWindowed ? riskTranscript : searchTranscript;
  const windowed = riskWindowed || searchActive ? windowedChoice : null;
  const active = windowed ?? transcript;
  // Prefer the enriched (cost/usage) normal-load chat, but fall back to the
  // active transcript's chat so a windowed view still renders if only that load
  // resolved (otherwise the panel would show "Not found" despite having data).
  const chat = transcript.chat ?? active.chat;
  const chatMessages = active.messages;
  const validFocusedMessageTurn =
    focusedMessageTurn !== undefined &&
    Number.isInteger(focusedMessageTurn) &&
    focusedMessageTurn > 0
      ? focusedMessageTurn
      : undefined;
  const transcriptMessageCount = transcript.messages.length;
  const transcriptLoading = transcript.isLoading;
  const transcriptFetchingNewer = transcript.isFetchingNewer;
  const transcriptHasMoreAfter = transcript.hasMoreAfter;
  const loadNextTranscriptPage = transcript.fetchNewer;

  // Provenance can cite a turn beyond the cheap first transcript page. Load
  // forward pages only until that raw one-based message index is available.
  useEffect(() => {
    if (
      validFocusedMessageTurn === undefined ||
      transcriptLoading ||
      transcriptFetchingNewer ||
      transcriptMessageCount >= validFocusedMessageTurn ||
      !transcriptHasMoreAfter
    ) {
      return;
    }
    loadNextTranscriptPage();
  }, [
    validFocusedMessageTurn,
    transcriptLoading,
    transcriptFetchingNewer,
    transcriptMessageCount,
    transcriptHasMoreAfter,
    loadNextTranscriptPage,
  ]);
  const focusedMessageId =
    validFocusedMessageTurn === undefined
      ? null
      : (transcript.messages[validFocusedMessageTurn - 1]?.id ?? null);
  const { data: membersData } = useMembers();
  const userLabel = chat
    ? chatOwnerLabel(
        membersData?.members,
        chat,
        user,
        personalAccountEmail(chat),
      )
    : "anonymous";
  // Same label as a node: unresolved owners get an explanatory tooltip in the
  // header's session details, while transcript rows keep the plain string.
  const userLabelNode = chat ? (
    <ChatOwnerLabel
      members={membersData?.members}
      chat={chat}
      currentUser={user}
      accountEmail={personalAccountEmail(chat)}
    />
  ) : (
    "anonymous"
  );
  // Only the primary (or risk) initial load blanks the whole panel; a search
  // re-fetch updates the transcript in place — its loading shows in the search
  // bar and as a "Searching…" empty state instead.
  const chatLoading =
    transcript.isLoading || (riskWindowed && riskTranscript.isLoading);
  const chatLoadHasErrors = active.isError;
  // Mirrors the `chat` fallback above: either load having 403'd is enough to
  // tell "you can't see this" apart from "this doesn't exist".
  const chatLoadForbidden = transcript.isForbidden || active.isForbidden;

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
    setRiskyOnly(false);
    setExclusionState(null);
    setPendingExclusionKey(null);
    setOptimisticExcluded(new Set());
    setSearchInput("");
    setSearchQuery("");
    setActiveOccurrenceKey(null);
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
      const anchorId = riskResultAnchorId(r);
      if (!anchorId) continue;

      const existing = map.get(anchorId);
      if (existing) existing.push(r);
      else map.set(anchorId, [r]);
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
  const claudeTurnByPromptId = useMemo(() => {
    const turns =
      chat?.agentUsage?.type === "claude"
        ? (chat.agentUsage.claude?.turns ?? [])
        : [];
    return buildClaudeTurnByPromptId(turns);
  }, [chat?.agentUsage]);

  const transcriptRows = useMemo(
    () => buildTranscript(chatMessages, chat?.contentParts ?? []),
    [chatMessages, chat?.contentParts],
  );
  const filterActive = riskyOnly;
  const visibleRows = useMemo(() => {
    if (!riskyOnly) return transcriptRows;
    // `is_risk` on each windowed message is the authorized "which messages are
    // risky" signal — it rides chat.load (no internal seq exposed), so the
    // filter works even when the org-admin-only risk.results.list (which powers
    // the match-detail badges) is forbidden. Fall back to per-message risk
    // results for safety.
    return transcriptRows.filter(
      (r) => rowHasRiskFlag(r) || rowIsFlagged(r, riskResultsByMessage),
    );
  }, [transcriptRows, riskyOnly, riskResultsByMessage]);
  const hasMoreBefore = active.hasMoreBefore;
  const hasMoreAfter = active.hasMoreAfter;
  const windowGaps = windowed?.gaps;
  const displayItems = useMemo(
    () =>
      buildDisplayItems({
        rows: visibleRows,
        hasMoreBefore,
        hasMoreAfter,
        gaps: windowGaps,
      }),
    [visibleRows, hasMoreBefore, hasMoreAfter, windowGaps],
  );
  const focusedItemIndex = useMemo(() => {
    if (!focusedMessageId) return null;
    const index = displayItems.findIndex((item) =>
      displayItemContainsMessage(item, focusedMessageId),
    );
    return index >= 0 ? index : null;
  }, [displayItems, focusedMessageId]);

  // Risk-review contexts (risk focus or the has-risk spotlight) open scrolled to
  // the first finding, however far down it is. Plain cost/default views open at
  // the top (first message) — even when the session happens to have findings.
  const initialScrollIndex = useMemo(() => {
    if (focusedItemIndex !== null) return focusedItemIndex;
    if (!dimNonRisk) return null;
    const idx = displayItems.findIndex((it) =>
      displayItemRows(it).some((r) => rowIsFlagged(r, riskResultsByMessage)),
    );
    return idx >= 0 ? idx : null;
  }, [focusedItemIndex, dimNonRisk, displayItems, riskResultsByMessage]);

  // Unified per-occurrence search navigation: flat-map the loaded display rows
  // into every query occurrence (message text / tool name / args / output) in
  // document order — mirroring exactly what the renderer highlights — so next/prev
  // steps each occurrence, not each matching message. (The `query` windowed load
  // decides which messages are loaded; here we navigate the rendered occurrences.)
  const occurrences = useMemo(() => {
    if (!searchActive) return EMPTY_OCCURRENCES;
    const out: Occurrence[] = [];
    displayItems.forEach((it, itemIndex) => {
      // Walk every row the item renders — a `toolGroup` holds a whole run of
      // them, and its members stay searchable while collapsed (a match inside
      // pins the group open).
      for (const row of displayItemRows(it)) {
        for (const f of rowSearchFields(
          row,
          searchQuery,
          riskResultsByMessage,
        )) {
          for (let k = 0; k < f.count; k++) {
            out.push({
              key: `${it.id}:${row.id}:${f.key}:${k}`,
              itemIndex,
              rowId: row.id,
              fieldKey: f.key,
              indexInField: k,
            });
          }
        }
      }
    });
    return out;
  }, [searchActive, displayItems, searchQuery, riskResultsByMessage]);
  const matchCount = occurrences.length;
  // A fresh result set (new query) snaps back to the first occurrence.
  useEffect(() => {
    setActiveOccurrenceKey(null);
  }, [searchQuery]);
  // Resolve the active occurrence by its stable key, falling back to the first
  // (key is null right after a query change, or stale if its row scrolled out of
  // the loaded window).
  const activeOccurrenceIdx = useMemo(() => {
    if (occurrences.length === 0) return 0;
    if (activeOccurrenceKey == null) return 0;
    const i = occurrences.findIndex((o) => o.key === activeOccurrenceKey);
    return i >= 0 ? i : 0;
  }, [occurrences, activeOccurrenceKey]);
  const activeOccurrence = occurrences[activeOccurrenceIdx] ?? null;
  const goToMatch = useCallback(
    (delta: number) => {
      if (occurrences.length === 0) return;
      const next =
        (activeOccurrenceIdx + delta + occurrences.length) % occurrences.length;
      setActiveOccurrenceKey(occurrences[next]?.key ?? null);
      // Bump so re-pressing next/prev re-scrolls even when the index is unchanged
      // (e.g. a single occurrence), since the scroll effect keys on this nonce.
      setScrollNonce((n) => n + 1);
    },
    [occurrences, activeOccurrenceIdx],
  );

  const transcriptPagination = useMemo<TranscriptPagination>(() => {
    return {
      hasMoreBefore,
      hasMoreAfter,
      // Scrolling near an edge streams single pages; the break buttons load
      // everything missing in their range in one action.
      onLoadOlder: () =>
        windowed ? windowed.loadBefore() : transcript.fetchOlder(),
      onLoadNewer: () =>
        windowed ? windowed.loadAfter() : transcript.fetchNewer(),
      onLoadAllOlder: () =>
        windowed ? windowed.loadAllBefore() : transcript.fetchOlder(),
      onLoadAllNewer: () =>
        windowed ? windowed.loadAllAfter() : transcript.loadRest(),
      isFetchingOlder: windowed
        ? windowed.loadingKey === "before"
        : transcript.isFetchingOlder,
      isFetchingNewer: windowed
        ? windowed.loadingKey === "after"
        : transcript.isFetchingNewer || transcript.isLoadingRest,
      onLoadGap: windowed ? windowed.loadGap : undefined,
      isLoadingGap: windowed
        ? (afterSeq: number) => windowed.loadingKey === `gap:${afterSeq}`
        : undefined,
      initialScrollIndex,
      // Every windowed view (risk focus, the risky-only toggle, or search) opens
      // mid-thread, so suppress the top-of-list auto-load + jump-to-start button
      // that the plain from-start transcript uses — otherwise the window's top
      // edge eagerly expands older messages on mount.
      scrollToFinding:
        riskWindowed || searchActive || validFocusedMessageTurn !== undefined,
      scrollToItemIndex: activeOccurrence?.itemIndex ?? focusedItemIndex,
      scrollNonce,
      activeOccurrence: activeOccurrence
        ? {
            itemIndex: activeOccurrence.itemIndex,
            rowId: activeOccurrence.rowId,
            fieldKey: activeOccurrence.fieldKey,
            indexInField: activeOccurrence.indexInField,
          }
        : null,
      focusedMessageId,
    };
  }, [
    hasMoreBefore,
    hasMoreAfter,
    riskWindowed,
    searchActive,
    validFocusedMessageTurn,
    windowed,
    transcript,
    initialScrollIndex,
    scrollNonce,
    activeOccurrence,
    focusedItemIndex,
    focusedMessageId,
  ]);

  // "Load all messages": shown whenever part of the conversation isn't loaded
  // (a paged-out tail, or un-expanded windowed segments/edges). One click
  // fetches every missing message.
  const fullyLoaded =
    !hasMoreBefore && !hasMoreAfter && (windowGaps?.size ?? 0) === 0;
  const loadingAllMessages = windowed
    ? windowed.loadingKey === "all"
    : transcript.isLoadingRest;
  const loadAllMessages = useCallback(() => {
    if (windowed) windowed.loadAll();
    else transcript.loadRest();
  }, [windowed, transcript]);

  useEffect(() => {
    if (view === "effects" && !fullyLoaded && !loadingAllMessages) {
      loadAllMessages();
    }
  }, [view, fullyLoaded, loadingAllMessages, loadAllMessages]);

  const userLabelOverride = chat ? userLabel : undefined;
  // The same key ChatOwnerLabel uses: a chat carries the Gram user when the
  // owner is a member and the reported agent id otherwise. Memoized because a
  // fresh object each render would invalidate the row context below on every
  // pass, re-rendering the whole transcript.
  const ownerIdentifier = useMemo(
    () =>
      chat?.userId
        ? { userId: chat.userId }
        : chat?.externalUserId
          ? { externalUserId: chat.externalUserId }
          : null,
    [chat?.userId, chat?.externalUserId],
  );

  const rowCtx = useMemo<RowContext>(
    () => ({
      riskResultsByMessage,
      claudeUsageByMessage,
      claudeToolUsageByToolUseId,
      claudeTurnByPromptId,
      dimNonRisk,
      searchQuery: searchActive ? searchQuery : undefined,
      userLabel: chat?.externalUserId,
      userLabelOverride,
      ownerIdentifier,
    }),
    [
      riskResultsByMessage,
      claudeUsageByMessage,
      claudeToolUsageByToolUseId,
      claudeTurnByPromptId,
      dimNonRisk,
      searchActive,
      searchQuery,
      chat?.externalUserId,
      userLabelOverride,
      ownerIdentifier,
    ],
  );

  // "Setup exclusion rule" swaps the transcript for the exclusion editor
  // in-place (with a back button) rather than stacking a second sheet on top.
  const openExclusion = useCallback((result: RiskResult) => {
    setExclusionState({ mode: "create", results: [result] });
    setPendingExclusionKey(findingKey(result));
    setView("exclusion");
  }, []);
  const closeExclusion = useCallback(() => {
    setView("chat");
    setExclusionState(null);
    setPendingExclusionKey(null);
  }, []);
  const handleExclusionDone = useCallback(() => {
    if (pendingExclusionKey) {
      setOptimisticExcluded((prev) => {
        const next = new Set(prev);
        next.add(pendingExclusionKey);
        return next;
      });
      // The server reconcile lags, so refetching chat.list still returns the old
      // per-session risk count. Optimistically drop this chat's count in the
      // Agent Sessions list cache by the findings the exclusion suppresses here.
      const removed =
        (riskData?.results ?? []).filter(
          (r) => findingKey(r) === pendingExclusionKey,
        ).length || 1;
      queryClient.setQueriesData<{ chats?: ChatOverview[] }>(
        { queryKey: ["@gram/client", "chat", "list"] },
        (old) => {
          if (!old?.chats) return old;
          return {
            ...old,
            chats: old.chats.map((c) =>
              c.id === chatId
                ? {
                    ...c,
                    riskFindingsCount: Math.max(
                      0,
                      (c.riskFindingsCount ?? 0) - removed,
                    ),
                  }
                : c,
            ),
          };
        },
      );
    }
    closeExclusion();
  }, [pendingExclusionKey, closeExclusion, riskData, queryClient, chatId]);

  if (chatLoading) {
    return (
      <div className="p-8">
        <SheetTitle>Loading</SheetTitle>
        <SheetDescription>Fetching chat session details...</SheetDescription>
      </div>
    );
  }

  if (!chat) {
    return chatLoadForbidden ? (
      <div className="p-8">
        <SheetTitle>Permission denied</SheetTitle>
        <SheetDescription>
          You don&apos;t have access to view this chat session.
        </SheetDescription>
      </div>
    ) : (
      <div className="p-8">
        <SheetTitle>Not found</SheetTitle>
        <SheetDescription>
          The selected chat session could not be found.
        </SheetDescription>
      </div>
    );
  }

  const error = logsError as Error | null;
  const pinned = Boolean(chat.pinned);
  const summary = localSummary ?? chat.summary;
  const summaryGeneratedAt = localSummaryGeneratedAt ?? chat.summaryGeneratedAt;

  return (
    <div className="bg-background flex h-full flex-col">
      <ChatDetailHeader
        chatId={chatId}
        chat={chat}
        userLabel={userLabelNode}
        messageCount={chat.numMessages}
        toolCount={toolLogs.length}
        canManageChat={canManageChat}
        compactMetadata={riskFocus}
        showFilter={view === "chat" || view === "effects"}
        condensed={view === "effects"}
        onCondensedChange={(on) => setView(on ? "effects" : "chat")}
        riskyOnly={riskyOnly}
        onRiskyOnlyChange={setRiskyOnly}
        showRiskyOnly={canViewRisk}
        searchBar={
          riskWindowed || view === "effects" ? undefined : (
            <ThreadSearchBar
              value={searchInput}
              onChange={setSearchInput}
              matchCount={matchCount}
              activeIndex={activeOccurrenceIdx}
              loading={searchActive && searchTranscript.isLoading}
              onPrev={() => goToMatch(-1)}
              onNext={() => goToMatch(1)}
            />
          )
        }
        pinned={pinned}
        pinPending={setPinnedMutation.isPending}
        onTogglePinned={() => {
          setPinnedMutation.mutate(
            {
              request: {
                setPinnedRequestBody: { id: chatId, pinned: !pinned },
              },
            },
            {
              onSettled: () => {
                void invalidateAllListChats(queryClient);
                void queryClient.invalidateQueries({
                  queryKey: ["chat", chatId, "transcript"],
                });
              },
              onError: (err) => {
                toast.error(err.message || "Failed to update pin");
              },
            },
          );
        }}
        onExport={() => {
          // Fetches the complete transcript server-side — the export must not
          // depend on which messages the panel happens to have loaded.
          void exportTraceDataAsJson({
            client,
            chatId,
            chat,
            telemetryLogLimit: PANEL_TELEMETRY_LOG_LIMIT,
            telemetryLogs: logs,
            riskResults,
          });
        }}
        onDelete={() => setShowDeleteConfirm(true)}
        onSetView={setView}
        onClose={onClose}
      />

      <SessionSummarySection
        key={chatId}
        chatId={chatId}
        summary={summary}
        summaryGeneratedAt={summaryGeneratedAt}
        onSummaryChange={(nextSummary, generatedAt, forChatId) => {
          if (forChatId !== chatId) {
            return;
          }
          setLocalSummary(nextSummary);
          setLocalSummaryGeneratedAt(generatedAt);
        }}
      />

      <SessionLinksSection
        key={`links-${chatId}`}
        chatId={chatId}
        onOpenChat={onOpenChat}
      />

      {chatLoadHasErrors && (
        <div className="border-destructive/30 bg-destructive/10 text-destructive border-b px-4 py-2 text-xs">
          Some messages failed to load. The transcript below may be incomplete.
        </div>
      )}

      {/* The transcript stays mounted while a sub-view (tools/exclusion) overlays
          it, so returning lands back at the same scroll position rather than
          re-scrolling to the first finding. */}
      <div className="relative flex flex-1 flex-col overflow-hidden">
        {!fullyLoaded && (
          <div className="bg-muted/30 flex items-center justify-center border-b px-4 py-1.5">
            <button
              type="button"
              disabled={loadingAllMessages}
              onClick={loadAllMessages}
              className="text-muted-foreground hover:text-foreground inline-flex cursor-pointer items-center gap-1.5 text-xs font-medium transition-colors disabled:cursor-default"
            >
              {loadingAllMessages ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <ChevronsDown className="size-3.5" />
              )}
              {loadingAllMessages
                ? "Loading all messages…"
                : "Load all messages"}
            </button>
          </div>
        )}
        <CreateExclusionContext.Provider
          value={canManageChat ? openExclusion : null}
        >
          <ChatTranscript
            key={chatId}
            items={displayItems}
            ctx={rowCtx}
            pagination={transcriptPagination}
            emptyMessage={
              searchActive
                ? searchTranscript.isLoading
                  ? "Searching…"
                  : `No messages match “${searchQuery}”.`
                : filterActive
                  ? "No messages match the current filter."
                  : dimNonRisk
                    ? "No flagged messages in this session."
                    : "No messages to display."
            }
          />
          {view === "effects" && (
            <div className="bg-background absolute inset-0 z-10 flex flex-col">
              <EffectsView
                chatId={chatId}
                rows={visibleRows}
                riskResultsByMessage={riskResultsByMessage}
                claudeToolUsageByToolUseId={claudeToolUsageByToolUseId}
                claudeTurnByPromptId={claudeTurnByPromptId}
                userLabel={chat?.externalUserId}
                userLabelOverride={userLabelOverride}
                isLoading={chatLoading || loadingAllMessages}
              />
            </div>
          )}
        </CreateExclusionContext.Provider>

        {view === "tools" && (
          <div className="bg-background absolute inset-0 z-10 flex flex-col">
            <SubViewBar title="Tool calls" onBack={() => setView("chat")} />
            <div className="flex-1 overflow-y-auto">
              <ToolCallsView
                toolLogs={toolLogs}
                isLoading={logsLoading}
                error={error}
              />
            </div>
          </div>
        )}

        {view === "exclusion" && exclusionState && (
          <div className="bg-background absolute inset-0 z-10 flex flex-col overflow-hidden">
            <SubViewBar
              title={
                exclusionState.mode === "edit"
                  ? "Edit exclusion rule"
                  : "Set up exclusion rule"
              }
              onBack={closeExclusion}
            />
            <p className="text-muted-foreground px-6 pt-4 text-sm">
              Suppress matching findings retroactively and going forward. Does
              not re-run analysis.
            </p>
            <ExclusionEditor
              state={exclusionState}
              onDone={handleExclusionDone}
            />
          </div>
        )}
      </div>

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
