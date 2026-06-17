import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactElement,
} from "react";
import { Link, Outlet, useNavigate, useParams } from "react-router";
import { AnimatePresence, motion } from "motion/react";
import { useAssistantRuntime, useAssistantState } from "@assistant-ui/react";
import { Chat } from "@gram-ai/elements";
import {
  ChevronLeft,
  Home,
  Loader2,
  MessageCircle,
  Minus,
  Plus,
  SquarePen,
} from "lucide-react";
import type { ChatOverview } from "@gram/client/models/components";
import { SortBy, SortOrder } from "@gram/client/models/operations/listchats";
import { useListChats } from "@gram/client/react-query";
import { useSession } from "@/contexts/Auth";
import {
  useHideInsightsDock,
  useInsightsState,
} from "@/components/insights-context";
import { useServerAssistantTransport } from "@/hooks/useServerAssistantTransport";
import { useSlugs } from "@/contexts/Sdk";
import {
  CHAT_LANDING_SUGGESTIONS,
  INSIGHTS_SUGGESTION_ICONS,
  SLASH_COMMANDS,
  type InsightsSuggestion,
} from "@/lib/insights-suggestions";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";

// Shared pill-style icon button used by the page chrome (back affordances).
const ICON_BUTTON_CLASS =
  "border-border text-muted-foreground hover:text-foreground hover:bg-accent flex items-center gap-1 rounded-full border px-2.5 py-1.5 transition-colors";

/** Layout route for `/chat`; renders the index (home) or a conversation. */
export function ChatRoot(): ReactElement {
  // The page IS the chat, so hide the floating dock across the /chat subtree.
  useHideInsightsDock();
  return <Outlet />;
}

/**
 * `/chat` landing — a full-page "Ask anything" entry point (a second way into
 * the Project Assistant alongside the docked composer).
 */
export function ChatHome(): ReactElement {
  const routes = useRoutes();
  return (
    <div className="relative flex h-full flex-col overflow-y-auto">
      <ChatLandingBackdrop />
      <div className="absolute top-4 left-4 z-10">
        <Link
          to={routes.home.href()}
          aria-label="Back to home"
          className={ICON_BUTTON_CLASS}
        >
          <ChevronLeft className="size-4" />
          <Home className="size-4" />
        </Link>
      </div>
      <div className="relative z-10 mx-auto flex w-full max-w-3xl flex-1 flex-col px-6 pt-[clamp(10rem,26vh,16rem)] pb-16">
        <ChatLanding autoFocusInput />
      </div>
    </div>
  );
}

/**
 * Decorative rainbow "powder burst" header for the full-page chat landing —
 * the Speakeasy brand rainbow, heavily blurred and masked so it fades out well
 * above the content. Purely ambient: aria-hidden + pointer-events-none, sat
 * behind everything, so it never gets in the way of the composer or list.
 */
function ChatLandingBackdrop(): ReactElement {
  return (
    <div
      aria-hidden="true"
      className="pointer-events-none absolute inset-x-0 top-0 z-0 h-[460px] overflow-hidden [mask-image:linear-gradient(to_bottom,black_30%,transparent_92%)]"
    >
      <div
        className="absolute top-[-160px] left-1/2 h-[560px] w-[920px] max-w-[140vw] -translate-x-1/2 opacity-60 blur-[72px] dark:opacity-40"
        style={{
          // Brand rainbow (matches INSIGHTS_AI_RAINBOW), each blob fading to its
          // own zero-alpha so the overlaps read as soft powder, not muddy grey.
          background: [
            "radial-gradient(38% 48% at 30% 42%, #C83228 0%, rgba(200,50,40,0) 70%)",
            "radial-gradient(36% 46% at 48% 28%, #FB873F 0%, rgba(251,135,63,0) 70%)",
            "radial-gradient(42% 52% at 64% 40%, #D2DC91 0%, rgba(210,220,145,0) 72%)",
            "radial-gradient(44% 54% at 70% 60%, #5A8250 0%, rgba(90,130,80,0) 72%)",
            "radial-gradient(42% 52% at 42% 62%, #2873D7 0%, rgba(40,115,215,0) 72%)",
            "radial-gradient(36% 46% at 26% 54%, #9BC3FF 0%, rgba(155,195,255,0) 72%)",
          ].join(","),
        }}
      />
    </div>
  );
}

// Example questions cycled through the composer placeholder — deliberately
// different from the suggestion chips below, to hint at the assistant's range.
const ASK_PLACEHOLDERS = [
  "Summarize this week's activity",
  "What should I look into today?",
  "How did usage change vs last week?",
  "Which agents are the most active?",
  "Show me recent failed tool calls",
  "What's my busiest MCP server?",
  "Draft a weekly usage recap",
];

const PLACEHOLDER_HOLD_MS = 4800;
const PLACEHOLDER_FADE_MS = 300;

/** Rotates the composer placeholder through ASK_PLACEHOLDERS, holding each then
 *  crossfading to the next. `visible` drives the fade; honours
 *  prefers-reduced-motion by holding on the first. */
function useCyclingPlaceholder(): { text: string; visible: boolean } {
  const [index, setIndex] = useState(0);
  const [visible, setVisible] = useState(true);
  useEffect(() => {
    if (window.matchMedia?.("(prefers-reduced-motion: reduce)").matches) return;
    let fadeId: ReturnType<typeof setTimeout>;
    const holdId = setInterval(() => {
      setVisible(false); // fade current out
      fadeId = setTimeout(() => {
        setIndex((n) => (n + 1) % ASK_PLACEHOLDERS.length);
        setVisible(true); // fade next in
      }, PLACEHOLDER_FADE_MS);
    }, PLACEHOLDER_HOLD_MS);
    return () => {
      clearInterval(holdId);
      clearTimeout(fadeId);
    };
  }, []);
  return { text: ASK_PLACEHOLDERS[index] ?? "Ask anything", visible };
}

/**
 * The "Ask anything" widget — greeting, composer, recents, recipes. Used by
 * the `/chat` landing and embedded on the project home page. Submitting opens
 * a fresh conversation on the shared runtime and navigates to the full-page
 * chat; the server mints the chat id on the first send.
 */
export function ChatLanding({
  autoFocusInput = false,
}: {
  autoFocusInput?: boolean;
}): ReactElement {
  const { user } = useSession();
  const navigate = useNavigate();
  const routes = useRoutes();
  const { sendPrompt } = useInsightsState();
  const [value, setValue] = useState("");
  const [inputFocused, setInputFocused] = useState(false);
  const [activeCommand, setActiveCommand] = useState(0);
  const { text: placeholder, visible: placeholderVisible } =
    useCyclingPlaceholder();

  // Slash menu: typing "/" opens a filtered palette of starter prompts. The
  // query is everything after the leading slash; matching is substring across
  // the command title, label, and full prompt.
  const slashQuery = value.startsWith("/") ? value.slice(1) : null;
  const slashCommands = useMemo(() => {
    if (slashQuery === null) return [];
    const q = slashQuery.trim().toLowerCase();
    if (!q) return SLASH_COMMANDS;
    return SLASH_COMMANDS.filter(
      (command) =>
        command.title.toLowerCase().includes(q) ||
        command.label.toLowerCase().includes(q) ||
        command.prompt.toLowerCase().includes(q),
    );
  }, [slashQuery]);
  const slashOpen = inputFocused && slashCommands.length > 0;

  // Reset the highlighted row whenever the query (and therefore the filtered
  // list) changes, so the selection never points past the end.
  useEffect(() => {
    setActiveCommand(0);
  }, [slashQuery]);

  const firstName = user.displayName?.trim().split(/\s+/)[0];
  const greeting = firstName
    ? `Hi ${firstName}, ask your Project Assistant about your AI usage`
    : "Ask your Project Assistant about your AI usage";

  const startChat = (prompt: string) => {
    const trimmed = prompt.trim();
    if (!trimmed) return;
    // Start the conversation on the shared runtime, then drop into the
    // full-page view — the queued prompt fires once the chat route mounts the
    // runtime. The server mints the chat id on the first send.
    sendPrompt(trimmed);
    void navigate(routes.chat.conversation.href("new"));
  };

  const submit = () => {
    if (value.startsWith("/")) {
      // Slash mode: Enter runs the highlighted command — never send the raw
      // "/…" text as a prompt.
      const command = slashCommands[activeCommand];
      if (command) startChat(command.prompt);
      return;
    }
    startChat(value);
  };

  const handleKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Escape" && value.startsWith("/")) {
      e.preventDefault();
      setValue("");
      return;
    }
    if (!slashOpen) return;
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActiveCommand((i) => (i + 1) % slashCommands.length);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActiveCommand(
        (i) => (i - 1 + slashCommands.length) % slashCommands.length,
      );
    }
  };

  return (
    <div className="flex w-full flex-col gap-6">
      <div className="flex flex-col gap-4">
        <h1 className="text-foreground text-3xl font-semibold tracking-tight">
          {greeting}
        </h1>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
          className="border-border bg-card focus-within:border-foreground/30 relative rounded-2xl border px-4 py-3 shadow-sm transition-colors"
        >
          <input
            value={value}
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={handleKeyDown}
            onFocus={() => setInputFocused(true)}
            onBlur={() => setInputFocused(false)}
            aria-label="Ask your Project Assistant about your AI usage"
            role="combobox"
            aria-expanded={slashOpen}
            aria-controls="ask-slash-menu"
            aria-activedescendant={
              slashOpen ? `ask-slash-${activeCommand}` : undefined
            }
            autoFocus={autoFocusInput}
            className="w-full bg-transparent text-base outline-none"
          />
          {/* Overlay placeholder so it can crossfade (native ::placeholder
              can't transition between values). Shown only while empty; the
              kbd hint advertises the slash menu. */}
          {value === "" && (
            <>
              <span
                aria-hidden="true"
                className="text-muted-foreground pointer-events-none absolute top-1/2 right-36 left-4 -translate-y-1/2 truncate text-base transition-opacity duration-300"
                style={{ opacity: placeholderVisible ? 1 : 0 }}
              >
                {placeholder}
              </span>
              <div className="text-muted-foreground pointer-events-none absolute top-1/2 right-3 -translate-y-1/2 flex items-center gap-1.5 text-xs">
                <kbd className="border-border rounded border px-1.5 py-0.5 font-mono">
                  /
                </kbd>
                for suggestions
              </div>
            </>
          )}
          {slashOpen && (
            <SlashCommandMenu
              commands={slashCommands}
              activeIndex={activeCommand}
              onHover={setActiveCommand}
              onSelect={(command) => startChat(command.prompt)}
            />
          )}
        </form>
      </div>

      <ChatHomeRecents />
      <ChatHomeSuggestions onPick={startChat} />
    </div>
  );
}

/**
 * Command palette rendered under the composer while the user is typing a slash
 * query. Selection (keyboard or hover) is owned by the parent so Enter and
 * clicks resolve to the same highlighted row. Items use onMouseDown-prevent so
 * clicking one doesn't blur the input (which would close the menu first).
 */
function SlashCommandMenu({
  commands,
  activeIndex,
  onHover,
  onSelect,
}: {
  commands: InsightsSuggestion[];
  activeIndex: number;
  onHover: (index: number) => void;
  onSelect: (command: InsightsSuggestion) => void;
}): ReactElement {
  const activeRef = useRef<HTMLButtonElement | null>(null);

  // Keep the keyboard-highlighted row visible. "nearest" is a no-op when the
  // row is already on-screen, so hovering visible rows never triggers a scroll.
  useEffect(() => {
    activeRef.current?.scrollIntoView({ block: "nearest" });
  }, [activeIndex]);

  return (
    <div
      id="ask-slash-menu"
      role="listbox"
      aria-label="Suggested prompts"
      className="border-border bg-card absolute inset-x-0 top-full z-20 mt-2 max-h-80 overflow-y-auto rounded-xl border p-1 shadow-lg"
    >
      {commands.map((command, index) => {
        const Icon = INSIGHTS_SUGGESTION_ICONS[command.icon ?? "sparkles"];
        const active = index === activeIndex;
        return (
          <button
            key={command.title}
            id={`ask-slash-${index}`}
            ref={active ? activeRef : undefined}
            type="button"
            role="option"
            aria-selected={active}
            // Prevent the input from blurring (which closes the menu) before
            // the click selects.
            onMouseDown={(e) => e.preventDefault()}
            onMouseEnter={() => onHover(index)}
            onClick={() => onSelect(command)}
            className={cn(
              "flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left",
              active ? "bg-muted" : "hover:bg-muted/60",
            )}
          >
            <Icon className="text-muted-foreground size-4 shrink-0" />
            <span className="flex min-w-0 flex-col">
              <span className="text-foreground truncate text-sm font-medium">
                {command.title}
              </span>
              <span className="text-muted-foreground truncate text-xs">
                {command.label}
              </span>
            </span>
          </button>
        );
      })}
    </div>
  );
}

// Collapsed Recents shows just the latest few as a flat list; "Show all"
// expands to every conversation grouped by time period.
const RECENTS_COLLAPSED_COUNT = 3;

// Ordered time buckets. A null label renders no header (today's chats sit
// directly under the "Recents" heading, matching the flat collapsed view).
const RECENT_PERIODS: {
  key: string;
  label: string | null;
  maxAgeDays: number;
}[] = [
  { key: "today", label: null, maxAgeDays: 0 },
  { key: "yesterday", label: "Yesterday", maxAgeDays: 1 },
  { key: "week", label: "Last week", maxAgeDays: 7 },
  { key: "month", label: "Last month", maxAgeDays: 30 },
  { key: "earlier", label: "Earlier", maxAgeDays: Infinity },
];

type RecentEntry =
  | { type: "header"; key: string; label: string }
  | { type: "row"; key: string; chat: ChatOverview };

function startOfDay(date: Date): number {
  const d = new Date(date);
  d.setHours(0, 0, 0, 0);
  return d.getTime();
}

/** Compact relative age for a row, e.g. "Just now", "56m", "3h", "4d", "2w". */
function formatRelativeTime(date: Date): string {
  const minutes = Math.floor((Date.now() - date.getTime()) / 60_000);
  if (minutes < 1) return "Just now";
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h`;
  const days = Math.floor(hours / 24);
  if (days < 7) return `${days}d`;
  const weeks = Math.floor(days / 7);
  if (weeks < 5) return `${weeks}w`;
  return date.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

interface ChatGroup {
  key: string;
  label: string | null;
  chats: ChatOverview[];
}

function groupChatsByPeriod(chats: ChatOverview[]): ChatGroup[] {
  const todayStart = startOfDay(new Date());
  const dayMs = 86_400_000;
  const groups: ChatGroup[] = RECENT_PERIODS.map((p) => ({
    key: p.key,
    label: p.label,
    chats: [],
  }));
  for (const chat of chats) {
    const ageDays = Math.floor(
      (todayStart - startOfDay(chat.lastMessageTimestamp)) / dayMs,
    );
    const index = RECENT_PERIODS.findIndex((p) => ageDays <= p.maxAgeDays);
    const group = groups[index === -1 ? groups.length - 1 : index];
    if (group) group.chats.push(chat);
  }
  return groups.filter((g) => g.chats.length > 0);
}

function buildRecentEntries(
  chats: ChatOverview[],
  showAll: boolean,
): RecentEntry[] {
  if (!showAll) {
    return chats
      .slice(0, RECENTS_COLLAPSED_COUNT)
      .map((chat) => ({ type: "row", key: `row:${chat.id}`, chat }));
  }
  const entries: RecentEntry[] = [];
  for (const group of groupChatsByPeriod(chats)) {
    if (group.label) {
      entries.push({
        type: "header",
        key: `header:${group.key}`,
        label: group.label,
      });
    }
    for (const chat of group.chats) {
      entries.push({ type: "row", key: `row:${chat.id}`, chat });
    }
  }
  return entries;
}

function ChatHomeRecents(): ReactElement {
  const { projectSlug } = useSlugs();
  // Reuse the dock's managed-assistant resolution to scope the list to this
  // project's Project Assistant conversations.
  const { assistantId, ready } = useServerAssistantTransport(
    projectSlug ?? "",
    true,
  );
  const { data } = useListChats(
    {
      assistantId: assistantId || undefined,
      sortBy: SortBy.LastMessageTimestamp,
      sortOrder: SortOrder.Desc,
      limit: 50,
    },
    undefined,
    { enabled: Boolean(ready && assistantId), throwOnError: false },
  );
  const chats = data?.chats ?? [];
  const [showAll, setShowAll] = useState(false);

  return (
    <section className="flex flex-col gap-2">
      <div className="flex items-center justify-between px-3">
        <h2 className="text-muted-foreground text-sm font-medium">
          Recent Chats
        </h2>
        {chats.length > RECENTS_COLLAPSED_COUNT && (
          <button
            type="button"
            onClick={() => setShowAll((v) => !v)}
            className="text-muted-foreground hover:text-foreground flex items-center gap-1 text-sm transition-colors"
          >
            {showAll ? "Show less" : "Show all"}
            {showAll ? (
              <Minus className="size-3.5" />
            ) : (
              <Plus className="size-3.5" />
            )}
          </button>
        )}
      </div>
      <RecentsBody chats={chats} loading={!data} showAll={showAll} />
    </section>
  );
}

function RecentsBody({
  chats,
  loading,
  showAll,
}: {
  chats: ChatOverview[];
  loading: boolean;
  showAll: boolean;
}): ReactElement {
  if (loading) {
    return (
      <p className="text-muted-foreground px-3 text-sm">
        Loading conversations…
      </p>
    );
  }
  if (chats.length === 0) {
    return (
      <p className="text-muted-foreground px-3 text-sm">
        Your recent conversations will appear here.
      </p>
    );
  }
  const entries = buildRecentEntries(chats, showAll);
  return (
    <motion.div layout className="flex flex-col">
      <AnimatePresence initial={false}>
        {entries.map((entry) => (
          <motion.div
            key={entry.key}
            layout
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.18, ease: "easeOut" }}
          >
            <RecentEntryView entry={entry} />
          </motion.div>
        ))}
      </AnimatePresence>
    </motion.div>
  );
}

function RecentEntryView({ entry }: { entry: RecentEntry }): ReactElement {
  if (entry.type === "header") {
    return (
      <h3 className="text-muted-foreground px-3 pt-4 pb-1 text-sm font-medium">
        {entry.label}
      </h3>
    );
  }
  return <RecentRow chat={entry.chat} />;
}

function RecentRow({ chat }: { chat: ChatOverview }): ReactElement {
  const routes = useRoutes();
  return (
    <Link
      to={routes.chat.conversation.href(chat.id)}
      className="hover:bg-accent flex items-center gap-3 rounded-lg px-3 py-1.5 transition-colors"
    >
      <span className="border-border bg-card text-muted-foreground flex size-9 shrink-0 items-center justify-center rounded-lg border">
        <MessageCircle className="size-4" />
      </span>
      <span className="text-foreground min-w-0 flex-1 truncate text-sm">
        {chat.title || "New chat"}
      </span>
      <span className="text-muted-foreground shrink-0 text-xs">
        {formatRelativeTime(chat.lastMessageTimestamp)}
      </span>
    </Link>
  );
}

function ChatHomeSuggestions({
  onPick,
}: {
  onPick: (prompt: string) => void;
}): ReactElement {
  return (
    <section className="flex flex-col gap-3">
      <h2 className="text-muted-foreground px-3 text-sm font-medium">
        Suggestions
      </h2>
      <div className="flex flex-wrap gap-x-2 gap-y-2.5 px-3">
        {CHAT_LANDING_SUGGESTIONS.map((suggestion) => {
          const SuggestionIcon =
            INSIGHTS_SUGGESTION_ICONS[suggestion.icon ?? "sparkles"];
          return (
            <button
              key={suggestion.title}
              type="button"
              onClick={() => onPick(suggestion.prompt)}
              className="border-border bg-card text-foreground hover:bg-accent hover:text-accent-foreground flex items-center gap-2 rounded-lg border px-3 py-2 text-sm transition-colors"
            >
              <SuggestionIcon className="size-4 shrink-0" />
              {suggestion.title}
            </button>
          );
        })}
      </div>
    </section>
  );
}

/**
 * `/chat/:chatId` — a single conversation. `:chatId` is either a server chat
 * id (opened by <SavedConversation> on the shared runtime) or the literal
 * `new` for the active/fresh thread (a seeded prompt is already streaming, or
 * an empty composer for a brand-new chat).
 */
export function ChatConversation(): ReactElement {
  const { chatId } = useParams();
  const routes = useRoutes();
  const navigate = useNavigate();
  const { assistantReady, newConversation } = useInsightsState();

  const startNewChat = () => {
    newConversation();
    void navigate(routes.chat.conversation.href("new"));
  };

  return (
    <div className="flex h-full flex-col">
      <header className="border-border flex shrink-0 items-center gap-3 border-b px-4 py-3">
        <Link
          to={routes.chat.href()}
          aria-label="Back to chat"
          className={ICON_BUTTON_CLASS}
        >
          <ChevronLeft className="size-4" />
        </Link>
        <div className="text-foreground min-w-0 flex-1 truncate text-left text-base font-medium">
          {assistantReady && <ChatConversationTitle />}
        </div>
        <button
          type="button"
          onClick={startNewChat}
          className="text-muted-foreground hover:text-foreground flex shrink-0 items-center gap-1.5 text-sm"
        >
          <SquarePen className="size-4" />
          New chat
        </button>
      </header>
      <div className="min-h-0 flex-1">
        <ConversationBody chatId={chatId} ready={assistantReady} />
      </div>
    </div>
  );
}

/**
 * The active conversation's title for the header. Reads the runtime's thread
 * list item, which assistant-ui updates live when the backend `generateTitle`
 * stream lands — so it flips from "New chat" to the generated title on its own.
 * Must render inside the shared runtime (gated on assistantReady).
 */
function ChatConversationTitle(): ReactElement {
  const title = useAssistantState(({ threadListItem }) => threadListItem.title);
  return <>{title || "New chat"}</>;
}

function ConversationBody({
  chatId,
  ready,
}: {
  chatId: string | undefined;
  ready: boolean;
}): ReactElement {
  // The shared runtime (mounted in InsightsProvider) is the ancestor here, so
  // gate on it rather than mounting a second provider — that's what lets an
  // in-flight turn started in the dock keep streaming after maximize.
  if (!ready) {
    return (
      <div className="flex h-full items-center justify-center">
        <Loader2 className="text-muted-foreground size-5 animate-spin" />
      </div>
    );
  }
  return <ConversationSurface chatId={chatId} />;
}

function ConversationSurface({
  chatId,
}: {
  chatId: string | undefined;
}): ReactElement {
  const activeRemoteId = useAssistantState(
    ({ threadListItem }) => threadListItem.remoteId ?? null,
  );
  // Render the active thread directly when it's a fresh chat ("new") or its id
  // already matches the URL — including right after a new chat mints its id and
  // the URL syncs from /chat/new to /chat/:id. Keeping <ChatSurface> at the same
  // position across that swap means the conversation isn't remounted. Only a
  // URL pointing at a DIFFERENT thread routes through <SavedConversation>.
  if (!chatId || chatId === "new" || chatId === activeRemoteId) {
    return (
      <>
        {chatId === "new" && <NewChatUrlSync activeRemoteId={activeRemoteId} />}
        <ChatSurface />
      </>
    );
  }
  return <SavedConversation chatId={chatId} />;
}

/**
 * When a brand-new chat (`/chat/new`) mints its server id, replace the URL with
 * `/chat/:id` so it's shareable and survives a reload. The shared runtime is
 * keyed independently of the route, so this is a pure address-bar update — no
 * remount.
 */
function NewChatUrlSync({
  activeRemoteId,
}: {
  activeRemoteId: string | null;
}): null {
  const navigate = useNavigate();
  const routes = useRoutes();
  useEffect(() => {
    if (!activeRemoteId) return;
    void navigate(routes.chat.conversation.href(activeRemoteId), {
      replace: true,
    });
  }, [activeRemoteId, navigate, routes]);
  return null;
}

// The provider renders its own `gram-elements h-full` wrapper (a plain block,
// not a flex column), so the chat surface fills it with `h-full`; using
// `flex-1` here would be inert and let the composer overflow off-screen.
function ChatSurface(): ReactElement {
  // `gram-chat-fullpage` lets the shared Elements customCss give the composer a
  // roomier height on the full page (via :host-context) without affecting the
  // compact docked panel — see CHAT_FULLPAGE_COMPOSER_CSS in insights-dock.
  return (
    <div className="gram-chat-fullpage h-full overflow-hidden">
      <Chat />
    </div>
  );
}

/**
 * Opens a saved conversation by id and holds the chat surface back until it is
 * the active thread. The Elements provider's own `history.initialThreadId`
 * switch fires on a fixed 100ms timer with a one-shot guard that races the
 * `chat.list` fetch, so on a cold load it silently no-ops. Switching once the
 * list has loaded (`threads.isLoading === false`) is reliable, and gating on
 * the active thread's remote id stops the empty/welcome thread flashing before
 * the conversation binds.
 */
function SavedConversation({ chatId }: { chatId: string }): ReactElement {
  const runtime = useAssistantRuntime();
  const isListLoading = useAssistantState(({ threads }) => threads.isLoading);
  const activeRemoteId = useAssistantState(
    ({ threadListItem }) => threadListItem.remoteId ?? null,
  );
  // Latch per chatId, not a one-shot boolean: navigating straight from one
  // saved conversation to another (same route, different :chatId) must switch
  // again, otherwise the view stays stuck on the previous thread's loader.
  const switchedForRef = useRef<string | null>(null);
  useEffect(() => {
    if (isListLoading || switchedForRef.current === chatId) return;
    switchedForRef.current = chatId;
    runtime.threads.switchToThread(chatId).catch(() => {
      // Allow a retry if the switch failed (e.g. list refetch in flight).
      switchedForRef.current = null;
    });
  }, [runtime, chatId, isListLoading]);

  if (activeRemoteId !== chatId) {
    return (
      <div className="flex h-full items-center justify-center">
        <Loader2 className="text-muted-foreground size-5 animate-spin" />
      </div>
    );
  }
  return <ChatSurface />;
}
