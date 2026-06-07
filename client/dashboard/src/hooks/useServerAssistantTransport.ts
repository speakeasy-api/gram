import { useEffect, useMemo, useRef, useState } from "react";
import {
  useEnsureManagedAssistantMutation,
  useGramContext,
} from "@gram/client/react-query";
import { createServerAssistantTransport } from "@/lib/ServerAssistantTransport";
import type { ElementsTransportFactory } from "@gram-ai/elements";

// How often to re-warm the runtime while the sidebar stays open. Kept under the
// server's warm TTL so the VM never goes cold between a user's messages while
// they read — eager warming is the dominant latency win over the send path.
const WARM_KEEPALIVE_MS = 45_000;

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

  // Latch invariant: `inflightSlugRef` holds the slug whose mutation is
  // currently in flight, or null. It is set at mutate kickoff and cleared in
  // onSettled, so a stale-slug callback never strands the latch.
  const inflightSlugRef = useRef<string | null>(null);
  const resolvedForSlugRef = useRef<string | null>(null);
  const currentSlugRef = useRef(projectSlug);
  currentSlugRef.current = projectSlug;

  // Project switch: drop any prior resolution so the new project resolves
  // fresh. The InsightsProvider lives above the route outlet and persists
  // across project navigation, so without this reset the transport would route
  // a project-A assistant for project B → 404. We track the last slug we
  // touched (resolved OR kicked off) so a mid-flight switch still resets.
  const trackedSlugRef = useRef<string | null>(null);
  useEffect(() => {
    if (
      trackedSlugRef.current !== null &&
      trackedSlugRef.current !== projectSlug
    ) {
      trackedSlugRef.current = null;
      resolvedForSlugRef.current = null;
      setAssistantId("");
      setError(null);
    }
  }, [projectSlug]);

  useEffect(() => {
    if (!enabled) {
      return;
    }
    if (!projectSlug) {
      return;
    }
    if (inflightSlugRef.current === projectSlug) {
      return;
    }
    if (resolvedForSlugRef.current === projectSlug) {
      return;
    }
    inflightSlugRef.current = projectSlug;
    trackedSlugRef.current = projectSlug;
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
        onSettled: () => {
          if (inflightSlugRef.current === slugAtRequest) {
            inflightSlugRef.current = null;
          }
        },
      },
    );
  }, [enabled, projectSlug, ensureManagedMutate]);

  const ready = assistantId !== "";

  // Keepalive: while the sidebar stays open, periodically re-ensure — which
  // re-warms the runtime VM and extends its warm window — so it doesn't go cold
  // between messages while the user reads. Fire-and-forget; only runs once the
  // assistant has resolved, and stops on close/unmount.
  useEffect(() => {
    if (!enabled || !ready || !projectSlug) {
      return;
    }
    const id = setInterval(() => {
      if (currentSlugRef.current !== projectSlug) {
        return;
      }
      ensureManagedMutate({});
    }, WARM_KEEPALIVE_MS);
    return () => clearInterval(id);
  }, [enabled, ready, projectSlug, ensureManagedMutate]);

  const transport = useMemo<ElementsTransportFactory | undefined>(() => {
    if (!ready) {
      return undefined;
    }
    return createServerAssistantTransport({ client, assistantId, projectSlug });
  }, [ready, client, assistantId, projectSlug]);

  return { transport, assistantId, ready, error };
}
