import { Page } from "@/components/page-layout";
import { useHideInsightsDock } from "@/components/insights-context";
import { useProject, useSession } from "@/contexts/Auth";
import { internalMcpUrl } from "@/hooks/useToolsetUrl";
import { DEFAULT_ASSISTANT_MODEL } from "@/lib/models";
import { getServerURL } from "@/lib/utils";
import {
  Chat,
  GramElementsProvider,
  useThreadId,
  type MCPServerEntry,
} from "@gram-ai/elements";
import { useListToolsets } from "@gram/client/react-query";
import { useChatSessionsCreateMutation } from "@gram/client/react-query/chatSessionsCreate.js";
import { ResizablePanel, useMoonshineConfig } from "@speakeasy-api/moonshine";
import { Loader2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef } from "react";
import { useParams, useSearchParams } from "react-router";
import { toast } from "sonner";
import { AssistantDraftProvider } from "./AssistantDraftContext";
import { useAssistantDraft } from "./useAssistantDraft";
import { AssistantDraftPanel } from "./AssistantDraftPanel";
import {
  buildSystemPrompt,
  buildWelcome,
  type AssistantSnapshot,
} from "./systemPrompt";
import { useOnboardingTools } from "./tools/useOnboardingTools";

export function NewAssistantOnboarding(): JSX.Element {
  const [searchParams, setSearchParams] = useSearchParams();
  const initialId = searchParams.get("id");

  const handleAssistantCreated = useCallback(
    (id: string) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          next.set("id", id);
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  return (
    <AssistantDraftProvider
      initialAssistantId={initialId}
      onAssistantCreated={handleAssistantCreated}
    >
      <OnboardingShell />
    </AssistantDraftProvider>
  );
}

export function EditAssistantOnboarding(): JSX.Element {
  const { assistantId = "" } = useParams();
  return (
    <AssistantDraftProvider initialAssistantId={assistantId}>
      <OnboardingShell />
    </AssistantDraftProvider>
  );
}

function OnboardingShell() {
  // Hosts its own chat runtime — hide the floating dock and keep the shared
  // runtime out of this tree (no nested RemoteThreadListRuntime).
  useHideInsightsDock();
  const draft = useAssistantDraft();
  const mode: "create" | "edit" = draft.assistantId ? "edit" : "create";
  const substitutions = useMemo(
    () =>
      draft.assistantId && draft.assistant?.name
        ? { [draft.assistantId]: draft.assistant.name }
        : undefined,
    [draft.assistantId, draft.assistant?.name],
  );

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs fullWidth substitutions={substitutions} />
      </Page.Header>
      <Page.Body fullWidth fullHeight className="p-0">
        <ResizablePanel
          direction="horizontal"
          className="[&>[role='separator']]:bg-neutral-softest [&>[role='separator']]:hover:bg-primary h-full [&>[role='separator']]:relative [&>[role='separator']]:w-px [&>[role='separator']]:border-0 [&>[role='separator']]:before:absolute [&>[role='separator']]:before:inset-y-0 [&>[role='separator']]:before:-right-1 [&>[role='separator']]:before:-left-1 [&>[role='separator']]:before:cursor-col-resize"
        >
          <ResizablePanel.Pane minSize={35}>
            <ChatPane mode={mode} />
          </ResizablePanel.Pane>
          <ResizablePanel.Pane minSize={24} defaultSize={36}>
            <AssistantDraftPanel />
          </ResizablePanel.Pane>
        </ResizablePanel>
      </Page.Body>
    </Page>
  );
}

function ChatPane({ mode }: { mode: "create" | "edit" }) {
  const session = useSession();
  const project = useProject();
  const draft = useAssistantDraft();
  const createSessionMutation = useChatSessionsCreateMutation();
  const { theme: resolvedTheme } = useMoonshineConfig();
  const [searchParams] = useSearchParams();

  const initialThreadId = searchParams.get("threadId") ?? undefined;

  // Once the assistant exists (create → after first save, or edit mode), link
  // setup threads to it: filter the thread list to this assistant's chats and
  // send the assistant id on completions so the server records a listable
  // assistant_threads row (source_kind=setup). Before the assistant exists the
  // draft has no id, so both stay undefined and the setup chat is an ordinary
  // unlinked chat until creation — reopening then starts a fresh thread, which
  // is acceptable.
  const assistantId = draft.assistantId ?? undefined;

  // Scope the onboarding history to this assistant's setup threads only
  // (source_kind=setup), so runtime threads (Slack/cron/etc.) for the same
  // assistant never leak into the onboarding list.
  const threadListFilters = useMemo(
    () =>
      assistantId
        ? { assistant_id: assistantId, source_kind: "setup" }
        : undefined,
    [assistantId],
  );

  const apiHeaders = useMemo<Record<string, string>>(
    () => ({
      "X-Gram-Source": "assistant",
      ...(assistantId ? { "Gram-Assistant-ID": assistantId } : {}),
    }),
    [assistantId],
  );

  const onboarding = useOnboardingTools();

  const { data: toolsetsData } = useListToolsets();
  const mcps = useMemo<MCPServerEntry[] | undefined>(() => {
    const fallbackEnv = draft.assistantEnv?.slug;
    const toolsetBySlug = new Map(
      (toolsetsData?.toolsets ?? []).map((t) => [t.slug, t]),
    );
    const entries: MCPServerEntry[] = [];
    for (const ref of draft.assistant?.toolsets ?? []) {
      const toolset = toolsetBySlug.get(ref.toolsetSlug);
      if (!toolset) continue;
      entries.push({
        url: internalMcpUrl({ slug: project.slug }, toolset),
        name: toolset.slug,
        environment: ref.environmentSlug ?? fallbackEnv,
      });
    }
    // Directly-attached MCP servers (no backing toolset) connect through the
    // same Gram-hosted /mcp/{endpoint} path the assistant runtime dials. No
    // fallback environment: most remote servers carry their own connection
    // auth, so only an explicitly bound environment is sent.
    for (const ref of draft.assistant?.mcpServers ?? []) {
      if (!ref.endpointSlug) continue;
      entries.push({
        url: `${getServerURL()}/mcp/${ref.endpointSlug}`,
        name: ref.mcpServerSlug,
        environment: ref.environmentSlug,
      });
    }
    return entries.length ? entries : undefined;
  }, [
    draft.assistant?.toolsets,
    draft.assistant?.mcpServers,
    draft.assistantEnv?.slug,
    toolsetsData?.toolsets,
    project.slug,
  ]);

  const getSession = useCallback(async () => {
    try {
      const result = await createSessionMutation.mutateAsync({
        request: {
          gramProject: project.id,
          createRequestBody: {
            embedOrigin: window.location.origin,
            expiresAfter: 3600,
            userIdentifier: session.user.id,
          },
        },
        security: {
          option1: {
            sessionHeaderGramSession: session.session,
            projectSlugHeaderGramProject: project.slug,
          },
        },
      });
      return result.clientToken;
    } catch (error) {
      toast.error("Failed to create chat session.");
      throw error;
    }
  }, [
    createSessionMutation,
    project.id,
    project.slug,
    session.session,
    session.user.id,
  ]);

  const snapshotRef = useRef<AssistantSnapshot | null>(null);
  if (mode === "edit" && !snapshotRef.current && draft.assistant) {
    snapshotRef.current = {
      name: draft.assistant.name,
      model: draft.assistant.model,
      status: draft.assistant.status,
      instructions: draft.assistant.instructions,
      toolsets: draft.assistant.toolsets.map((t) => ({
        slug: t.toolsetSlug,
        environmentSlug: t.environmentSlug ?? null,
      })),
    };
  }
  const snapshot = snapshotRef.current;

  const ready = mode === "create" || snapshot !== null;

  const systemPrompt = useMemo(() => {
    if (!ready) return null;
    return buildSystemPrompt({ mode, snapshot: snapshot ?? undefined });
  }, [mode, ready, snapshot]);

  const welcome = useMemo(
    () =>
      buildWelcome({
        mode,
        assistantName: snapshot?.name ?? draft.assistant?.name,
      }),
    [mode, snapshot?.name, draft.assistant?.name],
  );

  if (!systemPrompt) {
    return (
      <div className="flex h-full items-center justify-center">
        <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col">
      <GramElementsProvider
        config={{
          projectSlug: project.slug,
          api: {
            url: getServerURL(),
            session: getSession,
            headers: apiHeaders,
          },
          history: {
            enabled: true,
            // Surface prior setup/onboarding threads for this assistant so
            // reopening the page can resurface and revisit them. The list is
            // scoped to this assistant's chats; before the assistant exists the
            // filter is omitted and the list stays empty.
            showThreadList: true,
            ...(threadListFilters ? { threadListFilters } : {}),
            initialThreadId,
          },
          thread: {
            showFeedback: false,
          },
          variant: "standalone",
          systemPrompt,
          mcps,
          model: {
            defaultModel: DEFAULT_ASSISTANT_MODEL,
            showModelPicker: false,
          },
          welcome,
          composer: {
            placeholder:
              mode === "edit"
                ? `Message ${draft.assistant?.name ?? "your assistant"}…`
                : "Describe what you want this assistant to do…",
            toolMentions: false,
          },
          theme: {
            colorScheme: resolvedTheme === "dark" ? "dark" : "light",
            density: "normal",
            radius: "soft",
          },
          tools: {
            frontendTools: onboarding.frontendTools,
            components: onboarding.components,
            toolsRequiringApproval: onboarding.toolsRequiringApproval,
            maxOutputBytes: 50_000,
          },
        }}
      >
        <SetupThreadSync />
        <div className="h-full overflow-hidden">
          <Chat />
        </div>
      </GramElementsProvider>
    </div>
  );
}

// SetupThreadSync mirrors the active thread id into the `?threadId=` query
// param (replace-nav, so it doesn't spam history) so a setup/onboarding thread
// is URL-addressable and can be reopened via the shareable URL. Must render
// inside GramElementsProvider so useThreadId can read the active thread.
function SetupThreadSync() {
  const { threadId } = useThreadId();
  const [, setSearchParams] = useSearchParams();

  useEffect(() => {
    if (!threadId) return;
    setSearchParams(
      (prev) => {
        if (prev.get("threadId") === threadId) return prev;
        const next = new URLSearchParams(prev);
        next.set("threadId", threadId);
        return next;
      },
      { replace: true },
    );
  }, [threadId, setSearchParams]);

  return null;
}
