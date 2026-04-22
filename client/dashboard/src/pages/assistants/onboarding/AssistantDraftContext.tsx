import { Assistant } from "@gram/client/models/components/assistant.js";
import {
  invalidateAllAssistantsGet,
  invalidateAllAssistantsList,
  invalidateAllListEnvironments,
  invalidateAllListToolsets,
  invalidateAllTrigger,
  invalidateAllTriggers,
  queryKeyAssistantsGet,
  useAssistantsGet,
} from "@gram/client/react-query/index.js";
import { useQueryClient } from "@tanstack/react-query";
import {
  createContext,
  ReactNode,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";

type PendingResolver = (result: unknown) => void;

export type AssistantEnv = { id: string; slug: string };

type DraftContextValue = {
  assistantId: string | null;
  setAssistantId: (id: string) => void;
  setAssistant: (assistant: Assistant) => void;
  assistant: Assistant | undefined;
  refetchAssistant: () => Promise<unknown>;
  invalidateAll: () => void;
  registerPending: (toolCallId: string, resolver: PendingResolver) => void;
  resolvePending: (toolCallId: string, result: unknown) => boolean;
  assistantEnv: AssistantEnv | null;
  setAssistantEnv: (env: AssistantEnv | null) => void;
};

const Ctx = createContext<DraftContextValue | null>(null);

export function AssistantDraftProvider({
  initialAssistantId,
  onAssistantCreated,
  children,
}: {
  initialAssistantId: string | null;
  onAssistantCreated?: (id: string) => void;
  children: ReactNode;
}) {
  const queryClient = useQueryClient();
  const [assistantId, setAssistantIdState] = useState<string | null>(
    initialAssistantId,
  );
  const [assistantEnv, setAssistantEnvState] = useState<AssistantEnv | null>(
    null,
  );
  const pendingRef = useRef(new Map<string, PendingResolver>());

  const { data: assistant, refetch } = useAssistantsGet(
    { id: assistantId ?? "" },
    undefined,
    {
      enabled: !!assistantId,
      retry: false,
      throwOnError: false,
      refetchOnWindowFocus: false,
    },
  );

  const setAssistantId = useCallback(
    (id: string) => {
      setAssistantIdState((prev) => {
        if (prev === id) return prev;
        onAssistantCreated?.(id);
        return id;
      });
    },
    [onAssistantCreated],
  );

  const setAssistant = useCallback(
    (next: Assistant) => {
      queryClient.setQueryData(queryKeyAssistantsGet({ id: next.id }), next);
      setAssistantIdState((prev) => {
        if (prev === next.id) return prev;
        onAssistantCreated?.(next.id);
        return next.id;
      });
    },
    [queryClient, onAssistantCreated],
  );

  const invalidateAll = useCallback(() => {
    invalidateAllAssistantsGet(queryClient);
    invalidateAllAssistantsList(queryClient);
    invalidateAllListEnvironments(queryClient);
    invalidateAllListToolsets(queryClient);
    invalidateAllTriggers(queryClient);
    invalidateAllTrigger(queryClient);
  }, [queryClient]);

  const registerPending = useCallback(
    (toolCallId: string, resolver: PendingResolver) => {
      pendingRef.current.set(toolCallId, resolver);
    },
    [],
  );

  const resolvePending = useCallback((toolCallId: string, result: unknown) => {
    const resolver = pendingRef.current.get(toolCallId);
    if (!resolver) return false;
    pendingRef.current.delete(toolCallId);
    resolver(result);
    return true;
  }, []);

  const setAssistantEnv = useCallback((env: AssistantEnv | null) => {
    setAssistantEnvState(env);
  }, []);

  useEffect(() => {
    if (assistantEnv) return;
    const slugs = (assistant?.toolsets ?? [])
      .map((t) => t.environmentSlug)
      .filter((s): s is string => !!s);
    if (slugs.length === 0) return;
    const first = slugs[0]!;
    if (slugs.some((s) => s !== first)) return;
    setAssistantEnvState({ id: "", slug: first });
  }, [assistant, assistantEnv]);

  const value = useMemo<DraftContextValue>(
    () => ({
      assistantId,
      setAssistantId,
      setAssistant,
      assistant,
      refetchAssistant: refetch,
      invalidateAll,
      registerPending,
      resolvePending,
      assistantEnv,
      setAssistantEnv,
    }),
    [
      assistantId,
      setAssistantId,
      setAssistant,
      assistant,
      refetch,
      invalidateAll,
      registerPending,
      resolvePending,
      assistantEnv,
      setAssistantEnv,
    ],
  );

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useAssistantDraft(): DraftContextValue {
  const v = useContext(Ctx);
  if (!v) {
    throw new Error(
      "useAssistantDraft must be used inside an AssistantDraftProvider",
    );
  }
  return v;
}
