import { Page } from "@/components/page-layout";
import { useHideInsightsDock } from "@/components/insights-context";
import { Type } from "@/components/ui/type";
import { useProject, useSession } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { internalMcpUrl } from "@/hooks/useToolsetUrl";
import { getServerURL } from "@/lib/utils";
import {
  Chat,
  GramElementsProvider,
  type MCPServerEntry,
  type Model,
} from "@gram-ai/elements";
import { useListToolsets } from "@gram/client/react-query";
import { useChatSessionsCreateMutation } from "@gram/client/react-query/chatSessionsCreate.js";
import {
  Icon,
  ResizablePanel,
  useMoonshineConfig,
} from "@speakeasy-api/moonshine";
import { Loader2 } from "lucide-react";
import { ReactNode, useCallback, useMemo, useRef } from "react";
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
        <OnboardingEntryGate>
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
        </OnboardingEntryGate>
      </Page.Body>
    </Page>
  );
}

function OnboardingEntryGate({ children }: { children: ReactNode }) {
  const draft = useAssistantDraft();
  const { hasAllScopes, isLoading: rbacLoading } = useRBAC();

  if (draft.assistantStatus === "ready") return <>{children}</>;

  if (draft.assistantStatus === "loading" || rbacLoading) {
    return (
      <div className="flex h-full items-center justify-center">
        <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
      </div>
    );
  }

  const canCreate = hasAllScopes(["project:write", "mcp:write"]);
  if (!canCreate) return <AssistantUnavailableNotice />;

  return <>{children}</>;
}

function AssistantUnavailableNotice() {
  return (
    <div className="flex h-full min-h-[400px] w-full items-center justify-center">
      <div className="flex max-w-sm flex-col items-center gap-3 text-center">
        <div className="bg-muted flex h-12 w-12 items-center justify-center rounded-full">
          <Icon name="bot" className="text-muted-foreground h-5 w-5" />
        </div>
        <Type variant="subheading">No assistant yet</Type>
        <Type small muted>
          Ask an admin to set up an assistant for this project before you can
          chat with it.
        </Type>
      </div>
    </div>
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
            defaultModel: "anthropic/claude-opus-4.7" as Model,
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
        <div className="h-full overflow-hidden">
          <Chat />
        </div>
      </GramElementsProvider>
    </div>
  );
}
