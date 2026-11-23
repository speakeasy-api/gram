import { AssistantMarkdownLink } from "@/components/AssistantMarkdownLink";
import { GramLogo } from "@/components/gram-logo";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { Button } from "@/components/ui/Button";
import { useConfig } from "@/components/ui/hooks/useConfig";
import { useOrganization, useProject, useSession } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { useSlugs } from "@/contexts/Sdk";
import {
  Chat,
  GramElementsProvider,
  type ElementsConfig,
  type ElementsTransportFactory,
} from "@/elements";
import { useObservabilityMcpConfig } from "@/hooks/useObservabilityMcpConfig";
import { useServerAssistantTransport } from "@/hooks/useServerAssistantTransport";
import { useAssistantLinkResolver } from "@/lib/assistantEntityLinks";
import { stripMessageContextFraming } from "@/lib/projectAssistantTranscript";
import { cn } from "@/lib/utils";
import { ChatBridgeProvider } from "@/resources/kit/chat-bridge";
import { PageTitleProvider } from "@/resources/kit/page-title";
import {
  findResource,
  pickRoute,
  RESOURCES,
  routeToolCall,
} from "@/resources/registry";
import { useAui, useAuiState } from "@assistant-ui/react";
import type { UIMessage } from "ai";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router";
import { PROTOTYPE_CHAT_CSS } from "./chat-styles";
import { type OpenPage } from "./page-history";
import { SplitHandle } from "./SplitHandle";
import { useChatFraction } from "./use-chat-fraction";
import { ProjectSwitcher } from "./ProjectSwitcher";
import { pageAddress, usePageHistory } from "./page-history";

/**
 * Everything the assistant should know about where the user is, sent with
 * every message on the wire (never shown in the bubble). Written as plain
 * statements the model can act on.
 */
interface ChatContext {
  organization: { name: string; slug: string };
  project: { name: string; slug: string; id: string };
  user: { name: string; email: string };
  canChange: string[];
  page: {
    title: string;
    kind: string;
    address: string;
    recordId?: string;
  } | null;
}

function contextBlock(c: ChatContext): string {
  const pages = RESOURCES.map(
    (r) =>
      `- ${r.noun.many}: opens when you call ${Object.keys(r.chat.tools)
        .slice(0, 4)
        .join(", ")}`,
  ).join("\n");
  const lines = [
    `Organization: "${c.organization.name}" (slug: ${c.organization.slug}).`,
    `Current project: "${c.project.name}" (project_slug: ${c.project.slug}, id: ${c.project.id}). Use it whenever the user says "this project" or names no project; do not ask which project.`,
    `User: ${c.user.name} <${c.user.email}>.`,
    c.canChange.length > 0
      ? `The user can change: ${c.canChange.join(", ")}. Anything else, explain who can.`
      : "The user can view but not change things in this project.",
    c.page
      ? `Open page: the ${c.page.kind} page "${c.page.title}" at ${c.page.address}${c.page.recordId ? ` (id: ${c.page.recordId})` : ""}. "This", "it" and "here" refer to it.`
      : "Open page: the start page; nothing specific is open.",
    "A page beside this chat shows full results and opens automatically when you look things up. Do not repeat its contents as lists or tables. Reply in one to three sentences: what the page shows, anything notable, and the next step you suggest.",
    `Pages that exist:\n${pages}`,
  ];
  return `<dashboard_context>\n${lines.join("\n")}\n</dashboard_context>\n\n`;
}

interface PageContext {
  key: string;
  title: string;
  kind: string;
}
import { StartPage, SurfaceBrowser } from "./SurfaceBrowser";

/**
 * The standalone prototype: the conversation on the left and a surface on
 * the right, framed like a browser. The assistant opens pages on the
 * surface; with nothing open it shows a start page. Lives outside the app
 * chrome on purpose; every page is a resource mini app drawn by the kit.
 */
export default function PrototypePage(): JSX.Element {
  // A new project is a fresh session: chat, history and open page reset.
  const project = useProject();
  return <PrototypeShell key={project.slug} />;
}

function PrototypeShell(): JSX.Element {
  const project = useProject();
  const { orgSlug } = useSlugs();
  const includeAll = useCallback(() => true, []);
  const mcpConfig = useObservabilityMcpConfig({ toolsToInclude: includeAll });
  const assistant = useServerAssistantTransport(mcpConfig.projectSlug, true);
  const resolveLink = useAssistantLinkResolver();
  const { theme } = useConfig();

  const history = usePageHistory();
  const split = useChatFraction();
  const suggestions = useMemo(
    () => RESOURCES.flatMap((r) => r.chat.suggestions),
    [],
  );
  const page = history.page;
  const [recordTitle, setRecordTitle] = useState<string | null>(null);
  const [chatKey, setChatKey] = useState(0);
  // Below md there is room for one column: the chat, or the surface.
  const [mobileView, setMobileView] = useState<"chat" | "surface">("chat");
  const [queued, setQueued] = useState<{ text: string; nonce: number } | null>(
    null,
  );

  const resource = page ? findResource(page.resource) : undefined;
  const pageKey = page
    ? `${page.resource}:${page.location.view === "record" ? page.location.id : "list"}`
    : null;
  const pageTitle =
    page && resource
      ? page.location.view === "record"
        ? recordTitle
        : resource.noun.many
      : null;
  const context: PageContext | null =
    page && resource
      ? {
          key: pageKey ?? "",
          title: pageTitle ?? resource.noun.one,
          kind: page.location.view === "record" ? resource.noun.one : "List",
        }
      : null;

  // Tell the assistant what the user is looking at, on the wire only: the
  // message bubble shows what they typed.
  const organization = useOrganization();
  const { user } = useSession();
  const { hasScope } = useRBAC();
  const chatContext: ChatContext = {
    organization: { name: organization.name, slug: organization.slug },
    project: { name: project.name, slug: project.slug, id: project.id },
    user: {
      name: user.displayName ?? user.email.split("@")[0] ?? "",
      email: user.email,
    },
    canChange: [
      ["mcp:write", "MCP servers"],
      ["plugin:write", "plugins"],
      ["environment:write", "environments"],
      ["skill:write", "skills"],
      ["org:admin", "organization settings and access"],
    ].flatMap(([scope, label]) =>
      hasScope(scope as Parameters<typeof hasScope>[0], project.id)
        ? [label!]
        : [],
    ),
    page:
      page && context
        ? {
            title: context.title,
            kind: context.kind,
            address: pageAddress(project.slug, page),
            recordId:
              page.location.view === "record" ? page.location.id : undefined,
          }
        : null,
  };
  const chatContextRef = useRef(chatContext);
  chatContextRef.current = chatContext;
  const transport = useMemo<ElementsTransportFactory | undefined>(() => {
    const inner = assistant.transport;
    if (!inner) return undefined;
    return (ctx) => {
      const base = inner(ctx);
      return {
        sendMessages: async (args) => {
          const lastUser = args.messages.findLastIndex(
            (m) => m.role === "user",
          );
          if (lastUser === -1) return base.sendMessages(args);
          const original = args.messages[lastUser]!;
          const prefix = contextBlock(chatContextRef.current);
          let done = false;
          const parts = original.parts.map((p) => {
            if (done || p.type !== "text") return p;
            done = true;
            return { ...p, text: prefix + p.text };
          });
          if (!done) return base.sendMessages(args);
          const messages: UIMessage[] = [
            ...args.messages.slice(0, lastUser),
            { ...original, parts } as UIMessage,
            ...args.messages.slice(lastUser + 1),
          ];
          return base.sendMessages({ ...args, messages });
        },
        reconnectToStream: base.reconnectToStream.bind(base),
      };
    };
  }, [assistant.transport]);

  const config = useMemo<ElementsConfig>(
    () => ({
      ...mcpConfig,
      mcp: undefined,
      mcps: [],
      variant: "standalone",
      transport,
      resolveLink,
      linkComponent: AssistantMarkdownLink,
      allowMessageEdit: false,
      history: {
        enabled: true,
        threadListFilters: {
          assistant_id: assistant.assistantId,
          exclude_source_kind: "setup",
        },
        deferThreadIdMinting: true,
        transformChatMessage: stripMessageContextFraming,
      },
      api: {
        ...mcpConfig.api,
        headers: {
          ...mcpConfig.api?.headers,
          "X-Gram-Source": "dashboard-prototype",
        },
      },
      welcome: {
        title: "",
        subtitle: "",
        suggestions: suggestions.map((prompt) => ({
          title: prompt,
          label: "",
          prompt,
        })),
      },
      composer: { placeholder: "Message Speakeasy…" },
      theme: {
        colorScheme: theme === "dark" ? "dark" : "light",
        radius: "sharp",
        customCss: PROTOTYPE_CHAT_CSS,
      },
    }),
    [
      mcpConfig,
      transport,
      assistant.assistantId,
      resolveLink,
      theme,
      suggestions,
    ],
  );

  const bridge = useMemo(
    () => ({
      available: assistant.ready,
      ask: (text: string) => {
        setQueued({ text, nonce: Date.now() });
        setMobileView("chat");
      },
    }),
    [assistant.ready],
  );

  const { open: openInHistory } = history;
  const open = useCallback(
    (next: OpenPage | null) => {
      openInHistory(next);
      setMobileView("surface");
      if (next?.location.view !== "record") setRecordTitle(null);
    },
    [openInHistory],
  );

  return (
    <div className="bg-background text-foreground flex h-screen min-h-[600px] flex-col">
      <header className="flex h-16 shrink-0 items-center gap-3 px-6 pt-2">
        <GramLogo className="w-28" />
        <span aria-hidden="true" className="text-muted-foreground">
          /
        </span>
        <ProjectSwitcher />
        <div className="ml-auto flex items-center gap-4 text-sm">
          <div className="flex gap-1 md:hidden">
            <MobileTab
              active={mobileView === "chat"}
              onClick={() => setMobileView("chat")}
              label="Chat"
            />
            <MobileTab
              active={mobileView === "surface"}
              onClick={() => setMobileView("surface")}
              label="Page"
            />
          </div>
          <Link
            to={`/${orgSlug}/projects/${project.slug}`}
            className="text-muted-foreground hover:text-foreground hidden sm:inline"
          >
            Classic dashboard
          </Link>
        </div>
      </header>

      <div
        ref={split.containerRef}
        style={
          { "--chat-width": `${split.fraction * 100}%` } as React.CSSProperties
        }
        className={cn(
          "flex min-h-0 flex-1 overflow-hidden px-6 pb-6",
          split.dragging && "cursor-col-resize select-none",
        )}
      >
        <aside
          aria-label="Chat"
          className={cn(
            "min-h-0 w-full min-w-0 flex-col md:flex md:w-(--chat-width) md:min-w-[320px] md:shrink-0",
            mobileView === "chat" ? "flex" : "hidden",
          )}
        >
          {assistant.ready && transport ? (
            <GramElementsProvider key={chatKey} config={config}>
              <ChatBridgeProvider value={bridge}>
                <div className="relative flex h-full min-h-0 w-full flex-col">
                  <div
                    data-radius="sharp"
                    className="min-h-0 flex-1 overflow-hidden"
                  >
                    <Chat />
                  </div>
                  <ChatFooter
                    onNewChat={() => {
                      setChatKey((k) => k + 1);
                      open(null);
                    }}
                  />
                  <PromptSender
                    queued={queued}
                    onSent={() => setQueued(null)}
                  />
                  <ToolCallRouter onOpen={open} />
                </div>
              </ChatBridgeProvider>
            </GramElementsProvider>
          ) : (
            <AssistantStatus
              error={assistant.error}
              needsAdmin={assistant.needsAdmin}
              retry={assistant.retry}
            />
          )}
        </aside>

        <SplitHandle dragging={split.dragging} {...split.handleProps} />

        <main
          aria-label="Surface"
          className={cn(
            "bg-card min-h-0 min-w-0 flex-col border md:flex md:flex-1",
            mobileView === "surface" ? "flex flex-1" : "hidden",
          )}
        >
          <ChatBridgeProvider value={bridge}>
            <SurfaceBrowser
              history={{ ...history, open }}
              project={project.slug}
              title={pageTitle}
            >
              {page && resource ? (
                <div className="px-6 pt-8 pb-14 sm:px-10">
                  <div className="mx-auto max-w-[1400px]">
                    <ChatBridgeProvider value={bridge}>
                      <PageTitleProvider value={setRecordTitle}>
                        {resource.render({
                          location: page.location,
                          onNavigate: (location) =>
                            open({ resource: page.resource, location }),
                          onOpenRelated: (r, id) =>
                            open({
                              resource: r,
                              location: { view: "record", id },
                            }),
                          hideBackLink: true,
                        })}
                      </PageTitleProvider>
                    </ChatBridgeProvider>
                  </div>
                </div>
              ) : (
                <StartPage
                  history={{ ...history, open }}
                  project={project.slug}
                />
              )}
            </SurfaceBrowser>
          </ChatBridgeProvider>
        </main>
      </div>
    </div>
  );
}

/** Under the composer: the standing caveat, and a fresh start once talking. */
function ChatFooter({ onNewChat }: { onNewChat: () => void }): JSX.Element {
  const hasMessages = useAuiState(({ thread }) => thread.messages.length > 0);
  return (
    <div className="text-muted-foreground flex h-7 shrink-0 items-center justify-between px-0.5 text-xs">
      <span>Speakeasy can make mistakes</span>
      {hasMessages && (
        <button
          type="button"
          onClick={onNewChat}
          className="hover:text-foreground underline underline-offset-2"
        >
          New chat
        </button>
      )}
    </div>
  );
}

function MobileTab({
  active,
  onClick,
  label,
}: {
  active: boolean;
  onClick: () => void;
  label: string;
}): JSX.Element {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={cn(
        "px-2.5 py-1 text-sm transition-colors",
        active
          ? "bg-surface-tertiary-default text-foreground"
          : "text-muted-foreground hover:text-foreground",
      )}
    >
      {label}
    </button>
  );
}

function AssistantStatus({
  error,
  needsAdmin,
  retry,
}: {
  error: unknown;
  needsAdmin: boolean;
  retry: () => void;
}): JSX.Element {
  if (needsAdmin) {
    return (
      <InlineEmptyState
        icon="lock"
        heading="The assistant isn't set up for this project"
        description="Ask a project admin to open the assistant once. It sets itself up the first time."
      />
    );
  }
  if (error) {
    return (
      <InlineEmptyState
        icon="triangle-alert"
        heading="Couldn't reach the assistant"
        description="Pages still work on the right."
        action={
          <Button variant="secondary" size="sm" onClick={retry}>
            Try again
          </Button>
        }
      />
    );
  }
  return (
    <div
      role="status"
      className="text-muted-foreground flex h-full items-center justify-center text-sm"
    >
      Connecting to the assistant…
    </div>
  );
}

/**
 * Sends prompts that pages hand to the assistant ("Distribute this as a
 * plugin"). The thread can be a placeholder until history binds, and append
 * throws then, so a failed append waits for the next thread change.
 */
function PromptSender({
  queued,
  onSent,
}: {
  queued: { text: string; nonce: number } | null;
  onSent: () => void;
}): null {
  const thread = useAui().thread();
  const ready = useAuiState(
    ({ thread }) => !thread.isLoading && !thread.isDisabled,
  );
  const messageCount = useAuiState(({ thread }) => thread.messages.length);
  const sentRef = useRef<number | null>(null);

  useEffect(() => {
    if (!queued || !ready || sentRef.current === queued.nonce) return;
    try {
      thread.append(queued.text);
      sentRef.current = queued.nonce;
      onSent();
    } catch {
      // Placeholder thread: retry when the real one binds.
    }
  }, [queued, ready, messageCount, thread, onSent]);

  return null;
}

/**
 * Opens the page a tool call is about, once, when its result arrives. Calls
 * already in the thread when it loads don't count, so opening an old chat
 * never yanks the surface around.
 */
function ToolCallRouter({
  onOpen,
}: {
  onOpen: (page: OpenPage) => void;
}): null {
  const messages = useAuiState(({ thread }) => thread.messages);
  const seen = useRef<Set<string> | null>(null);

  useEffect(() => {
    const calls: {
      id: string;
      name: string;
      args: unknown;
      result: unknown;
    }[] = [];
    let userText = "";
    for (const message of messages) {
      for (const part of message.content) {
        if (message.role === "user" && part.type === "text") {
          userText = part.text;
        }
        if (part.type === "tool-call" && part.result !== undefined) {
          calls.push({
            id: part.toolCallId,
            name: part.toolName,
            args: part.args,
            result: part.result,
          });
        }
      }
    }
    if (seen.current == null) {
      seen.current = new Set(calls.map((c) => c.id));
      return;
    }
    const fresh = calls.filter((c) => !seen.current!.has(c.id));
    for (const c of fresh) seen.current.add(c.id);
    const route = pickRoute(
      fresh.flatMap((c) => routeToolCall(c.name, c.args, c.result)),
      userText,
    );
    if (route) onOpen(route);
  }, [messages, onOpen]);

  return null;
}
