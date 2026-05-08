import { Page } from "@/components/page-layout";
import { useProject, useSession } from "@/contexts/Auth";
import { useToolset } from "@/hooks/toolTypes";
import { useInternalMcpUrl } from "@/hooks/useToolsetUrl";
import { getServerURL } from "@/lib/utils";
import { Chat, GramElementsProvider, type Model } from "@gram-ai/elements";
import { useChatSessionsCreateMutation } from "@gram/client/react-query/chatSessionsCreate.js";
import { ResizablePanel, useMoonshineConfig } from "@speakeasy-api/moonshine";
import { Loader2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
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

export function NewAssistantOnboarding() {
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

export function EditAssistantOnboarding() {
  const { assistantId = "" } = useParams();
  return (
    <AssistantDraftProvider initialAssistantId={assistantId}>
      <OnboardingShell />
    </AssistantDraftProvider>
  );
}

function OnboardingShell() {
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
          <ResizablePanel.Pane minSize={35} order={0}>
            <ChatPane mode={mode} />
          </ResizablePanel.Pane>
          <ResizablePanel.Pane minSize={20} defaultSize={28}>
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

  const onboarding = useOnboardingTools();

  const firstToolset = draft.assistant?.toolsets[0];
  const { data: firstToolsetData } = useToolset(firstToolset?.toolsetSlug);
  const mcpUrl = useInternalMcpUrl(firstToolsetData);
  const gramEnvironment = firstToolset
    ? (firstToolset.environmentSlug ?? draft.assistantEnv?.slug)
    : undefined;

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

  const [snapshot, setSnapshot] = useState<AssistantSnapshot | null>(null);

  useEffect(() => {
    if (mode !== "edit") return;
    if (snapshot) return;
    if (!draft.assistant) return;
    setSnapshot({
      name: draft.assistant.name,
      model: draft.assistant.model,
      status: draft.assistant.status,
      instructions: draft.assistant.instructions,
      toolsets: draft.assistant.toolsets.map((t) => ({
        slug: t.toolsetSlug,
        environmentSlug: t.environmentSlug ?? null,
      })),
    });
  }, [mode, draft.assistant, snapshot]);

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
          mcp: mcpUrl,
          gramEnvironment,
          model: {
            defaultModel: "anthropic/claude-sonnet-4.6" as Model,
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
