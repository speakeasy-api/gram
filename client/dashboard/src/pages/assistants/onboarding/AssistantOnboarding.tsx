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
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useParams, useSearchParams } from "react-router";
import { toast } from "sonner";
import { AssistantDraftProvider } from "./AssistantDraftContext";
import { useAssistantDraft } from "./useAssistantDraft";
import { AssistantDraftPanel } from "./AssistantDraftPanel";
import {
  readStoredSetupThreadId,
  writeStoredSetupThreadId,
} from "./setupThreadMemory";
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

  // Restore the assistant's setup conversation on reopen: an explicit
  // ?threadId= (e.g. a shared link) wins, otherwise fall back to the thread
  // remembered for this assistant (written by SetupThreadSync below). Captured
  // once at mount — the provider only consumes initialThreadId once, and
  // re-reading storage after SetupThreadSync starts writing would churn the
  // config for no reason. In create mode there is no assistant id yet, so a
  // brand-new assistant always starts a fresh thread.
  const draftAssistantId = draft.assistantId;
  const [storedThreadId] = useState(() =>
    draftAssistantId
      ? readStoredSetupThreadId(project.id, session.user.id, draftAssistantId)
      : undefined,
  );
  const initialThreadId = searchParams.get("threadId") ?? storedThreadId;

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
        <SetupThreadSync assistantId={draftAssistantId} />
        <div className="h-full overflow-hidden">
          <Chat />
        </div>
      </GramElementsProvider>
    </div>
  );
}

/**
 * Keeps the assistant's setup-thread pointer fresh: whenever the active chat
 * thread gains a persisted id, record it against the assistant (localStorage)
 * and mirror it into the ?threadId= URL param, so reopening or reloading this
 * assistant resumes the same conversation instead of starting a blank one.
 * Must render inside GramElementsProvider — useThreadId reads the chat
 * runtime. Renders nothing.
 *
 * In the create flow assistantId starts null and is set once the assistant is
 * created mid-chat; the effect re-runs at that point and records the thread
 * under the new assistant's key.
 */
function SetupThreadSync({
  assistantId,
}: {
  assistantId: string | null;
}): null {
  const session = useSession();
  const project = useProject();
  const { threadId } = useThreadId();
  const [searchParams, setSearchParams] = useSearchParams();

  const urlThreadId = searchParams.get("threadId");
  useEffect(() => {
    // threadId is null until the thread is persisted (first message sent) —
    // don't record empty threads.
    if (!threadId) return;
    if (assistantId) {
      writeStoredSetupThreadId(
        project.id,
        session.user.id,
        assistantId,
        threadId,
      );
    }
    if (urlThreadId !== threadId) {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          next.set("threadId", threadId);
          return next;
        },
        { replace: true },
      );
    }
  }, [
    threadId,
    assistantId,
    project.id,
    session.user.id,
    urlThreadId,
    setSearchParams,
  ]);

  return null;
}
