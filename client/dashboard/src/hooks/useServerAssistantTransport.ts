import { useEffect, useMemo, useRef, useState } from "react";
import {
  useEnsureManagedAssistantMutation,
  useGramContext,
} from "@gram/client/react-query";
import { createServerAssistantTransport } from "@/lib/ServerAssistantTransport";
import type { ElementsTransportFactory } from "@gram-ai/elements";

export interface UseServerAssistantTransportResult {
  /**
   * Transport factory for `ElementsConfig.transport`, available once the managed
   * assistant has resolved. Undefined while connecting (or after a failure) so
   * the caller can gate the chat instead of falling back to client-side
   * generation.
   */
  transport: ElementsTransportFactory | undefined;
  /** The project's managed assistant id — used to scope the conversation list. */
  assistantId: string;
  /** Whether the managed assistant has resolved and the transport is live. */
  ready: boolean;
  /** Connection error message, if resolving the managed assistant failed. */
  error: string | null;
}

/**
 * Resolves the project's server-side Project Assistant (provisioning it on first
 * access) and exposes a transport factory wired to it. The conversation id,
 * history, and conversation list are owned by Elements' RemoteThreadListAdapter
 * (backed by the chat service), so this hook only resolves the assistant and
 * builds the send transport.
 *
 * Resolution is lazy — only once `enabled` first becomes true (the sidebar is
 * opened) — so we never provision an assistant for users who never open it. The
 * provider is expected to mount inside the sidebar, so closing and reopening
 * after a failure retries.
 */
export function useServerAssistantTransport(
  projectSlug: string,
  enabled: boolean,
): UseServerAssistantTransportResult {
  const client = useGramContext();
  const ensureManaged = useEnsureManagedAssistantMutation();
  const ensureManagedMutate = ensureManaged.mutate;

  const [assistantId, setAssistantId] = useState<string>("");
  const [error, setError] = useState<string | null>(null);

  const requestedRef = useRef(false);
  const resolvedForSlugRef = useRef<string | null>(null);
  const currentSlugRef = useRef(projectSlug);
  currentSlugRef.current = projectSlug;

  // Project switch: drop the previously-resolved assistant so the new project's
  // managed assistant gets resolved fresh. The InsightsProvider lives above the
  // route outlet and persists across project navigation, so without this reset
  // the transport would route assistant from project A to project B → 404.
  useEffect(() => {
    if (
      resolvedForSlugRef.current !== null &&
      resolvedForSlugRef.current !== projectSlug
    ) {
      resolvedForSlugRef.current = null;
      requestedRef.current = false;
      setAssistantId("");
      setError(null);
    }
  }, [projectSlug]);

  useEffect(() => {
    if (!enabled) {
      if (!assistantId) {
        requestedRef.current = false;
      }
      return;
    }
    if (requestedRef.current || !projectSlug) {
      return;
    }
    requestedRef.current = true;
    setError(null);
    const slugAtRequest = projectSlug;
    ensureManagedMutate(
      {},
      {
        onSuccess: (assistant) => {
          if (slugAtRequest !== currentSlugRef.current) {
            return;
          }
          resolvedForSlugRef.current = slugAtRequest;
          setAssistantId(assistant.id);
        },
        onError: () => {
          if (slugAtRequest !== currentSlugRef.current) {
            return;
          }
          setError(
            "Couldn't connect to the Project Assistant. Try reopening the sidebar.",
          );
        },
      },
    );
  }, [enabled, projectSlug, ensureManagedMutate, assistantId]);

  const ready = assistantId !== "";

  const transport = useMemo<ElementsTransportFactory | undefined>(() => {
    if (!ready) {
      return undefined;
    }
    return createServerAssistantTransport({ client, assistantId, projectSlug });
  }, [ready, client, assistantId, projectSlug]);

  return { transport, assistantId, ready, error };
}
