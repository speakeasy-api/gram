import { Page } from "@/components/page-layout";
import { useHideInsightsDock } from "@/components/insights-context";
import { useProject, useSession } from "@/contexts/Auth";
import { internalMcpUrl } from "@/hooks/useToolsetUrl";
import { DEFAULT_ASSISTANT_MODEL } from "@/lib/models";
import { getServerURL } from "@/lib/utils";
import {
  Chat,
  GramElementsProvider,
  type MCPServerEntry,
} from "@gram-ai/elements";
import { useListToolsets } from "@gram/client/react-query";
import { useChatSessionsCreateMutation } from "@gram/client/react-query/chatSessionsCreate.js";
import { ResizablePanel, useMoonshineConfig } from "@speakeasy-api/moonshine";
import { useAssistantRuntime, useAssistantState } from "@assistant-ui/react";
import { Loader2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useParams, useSearchParams } from "react-router";
import { toast } from "sonner";
import { AssistantDraftProvider } from "./AssistantDraftContext";
import {
  clearSetupThreadId,
  readSetupThreadId,
  writeSetupThreadId,
} from "./setupThreadStorage";
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

  // The chat backing this assistant's setup conversation, resolved once at
  // mount: an explicit ?threadId (a shared link) wins, otherwise the thread we
  // remembered for this assistant so reopening its setup restores the history.
  // undefined starts a fresh thread — the case in "create" mode, where no
  // assistant exists yet.
  const [setupThreadId] = useState<string | undefined>(() => {
    const fromUrl = searchParams.get("threadId");
    if (fromUrl) return fromUrl;
    return draft.assistantId
      ? (readSetupThreadId(project.id, draft.assistantId) ?? undefined)
      : undefined;
  });

  const onboarding = useOnboardingTools();

  const { data: toolsetsData } = useListToolsets();
  const mcps = useMemo<MCPServerEntry[] | undefined>(() => {
    const refs = draft.assistant?.toolsets;
    if (!refs?.length) return undefined;
    const fallbackEnv = draft.assistantEnv?.slug;
    const toolsetBySlug = new Map(
      (toolsetsData?.toolsets ?? []).map((t) => [t.slug, t]),
    );
    const entries: MCPServerEntry[] = [];
    for (const ref of refs) {
      const toolset = toolsetBySlug.get(ref.toolsetSlug);
      if (!toolset) continue;
      entries.push({
        url: internalMcpUrl({ slug: project.slug }, toolset),
        name: toolset.slug,
        environment: ref.environmentSlug ?? fallbackEnv,
      });
    }
    return entries.length ? entries : undefined;
  }, [
    draft.assistant?.toolsets,
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
            headers: { "X-Gram-Source": "assistant" },
          },
          history: {
            enabled: true,
            showThreadList: false,
            // Reopening the remembered thread is driven by <SetupChatSurface>
            // below rather than config.initialThreadId: the built-in switch
            // fires on a fixed 100ms timer that races chat.list and silently
            // no-ops on a cold load, which is exactly the reopen path here.
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
        <SetupChatSurface
          projectId={project.id}
          assistantId={draft.assistantId}
          target={setupThreadId}
        />
      </GramElementsProvider>
    </div>
  );
}

// assistant-ui's prefix for local (unpersisted) thread ids. `remoteId` is
// normally a server chat id, but guard against ever persisting a local id.
const LOCAL_THREAD_ID_PREFIX = "__LOCALID_";

// Upper bound on how long the surface waits for a reopen switch before showing
// the chat anyway, so a slow or wedged switch never blocks the composer.
const REOPEN_TIMEOUT_MS = 6000;

/**
 * Hosts the onboarding <Chat> and keeps the setup conversation stable across
 * reopens. Rendered inside GramElementsProvider so it can drive the runtime:
 *
 * - Reopen: when `target` names a remembered thread, switch to it once the
 *   thread list has settled (the same reliable pattern the full chat page uses,
 *   rather than the racy config.initialThreadId timer). A loader holds the
 *   surface back until the thread binds, with a failure/timeout escape so a
 *   stale id never wedges the page.
 * - Persist: as the active thread adopts a server chat id, remember it against
 *   the assistant so the next open reopens the same conversation.
 */
function SetupChatSurface({
  projectId,
  assistantId,
  target,
}: {
  projectId: string;
  assistantId: string | null;
  target: string | undefined;
}): JSX.Element {
  const runtime = useAssistantRuntime();
  const isListLoading = useAssistantState(({ threads }) => threads.isLoading);
  const activeRemoteId = useAssistantState(
    ({ threadListItem }) => threadListItem.remoteId ?? null,
  );

  // Nothing to reopen (create mode, first-time setup, shared-link miss) → ready
  // immediately. Otherwise hold back until the target thread is active.
  const [reopened, setReopened] = useState(!target);
  const switchStartedRef = useRef(false);

  useEffect(() => {
    if (!target || switchStartedRef.current || isListLoading) return;
    switchStartedRef.current = true;
    if (activeRemoteId === target) {
      setReopened(true);
      return;
    }
    runtime.threads
      .switchToThread(target)
      .then(() => setReopened(true))
      .catch(() => {
        // The remembered chat no longer resolves (deleted, wrong project) —
        // forget it and fall back to the fresh thread already active.
        if (assistantId) clearSetupThreadId(projectId, assistantId);
        setReopened(true);
      });
  }, [target, isListLoading, activeRemoteId, runtime, assistantId, projectId]);

  useEffect(() => {
    if (reopened) return;
    const timer = setTimeout(() => setReopened(true), REOPEN_TIMEOUT_MS);
    return () => clearTimeout(timer);
  }, [reopened]);

  // Remember the active thread against the assistant for the next reopen. Skips
  // local ids and waits until the assistant exists (created mid-conversation in
  // "create" mode), so the first thing persisted is a real, reopenable chat.
  useEffect(() => {
    if (
      !assistantId ||
      !activeRemoteId ||
      activeRemoteId.startsWith(LOCAL_THREAD_ID_PREFIX)
    ) {
      return;
    }
    writeSetupThreadId(projectId, assistantId, activeRemoteId);
  }, [assistantId, activeRemoteId, projectId]);

  const showLoader = !reopened && activeRemoteId !== target;

  return (
    <div className="h-full overflow-hidden">
      {showLoader ? (
        <div className="flex h-full items-center justify-center">
          <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
        </div>
      ) : (
        <Chat />
      )}
    </div>
  );
}
