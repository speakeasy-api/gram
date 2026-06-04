import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  useEnsureManagedAssistantMutation,
  useGramContext,
} from "@gram/client/react-query";
import { ServerAssistantTransport } from "@/lib/ServerAssistantTransport";

function correlationStorageKey(projectSlug: string): string {
  return `gram.projectAssistant.correlation.${projectSlug}`;
}

/**
 * Reads the persisted correlation id for a project, creating (and persisting)
 * one on first use. Falls back to an ephemeral id if localStorage is
 * unavailable (e.g. private browsing).
 */
function loadOrCreateCorrelationId(projectSlug: string): string {
  const key = correlationStorageKey(projectSlug);
  try {
    const existing = localStorage.getItem(key);
    if (existing) return existing;
  } catch {
    return crypto.randomUUID();
  }
  const fresh = crypto.randomUUID();
  try {
    localStorage.setItem(key, fresh);
  } catch {
    // Persisting failed; the id still works for this session.
  }
  return fresh;
}

export interface UseServerAssistantTransportResult {
  /**
   * The server-backed transport, available once the managed assistant has been
   * resolved. Undefined while connecting so the caller can withhold it (and
   * keep the UI in a connecting state) rather than fall back to client-side
   * generation.
   */
  transport: ServerAssistantTransport | undefined;
  /** Whether the managed assistant has been resolved and the transport is live. */
  ready: boolean;
  /** Connection error message, if resolving the managed assistant failed. */
  error: string | null;
  /**
   * Starts a brand-new conversation: rotates the correlation id (so the server
   * opens a fresh thread). The caller is responsible for clearing the visible
   * chat (e.g. by remounting the chat provider).
   */
  startFresh: () => void;
}

/**
 * Owns the lifecycle of the project's server-side Project Assistant for the
 * insights sidebar: resolves (provisioning on first access) the managed
 * assistant, persists a per-project correlation id so the conversation survives
 * reloads, and exposes a stable {@link ServerAssistantTransport} wired to both.
 *
 * The assistant is resolved lazily — only once `enabled` first becomes true
 * (i.e. the sidebar is opened) — so we never provision an assistant for users
 * who never open it.
 */
export function useServerAssistantTransport(
  projectSlug: string,
  enabled: boolean,
): UseServerAssistantTransportResult {
  const client = useGramContext();
  const ensureManaged = useEnsureManagedAssistantMutation();

  // Correlation id lives in a ref so rotating it (Start fresh) doesn't force the
  // transport to be recreated — getCorrelationId reads the latest value lazily.
  const correlationRef = useRef<string>("");
  if (!correlationRef.current && projectSlug) {
    correlationRef.current = loadOrCreateCorrelationId(projectSlug);
  }
  const getCorrelationId = useCallback(() => correlationRef.current, []);

  // A single transport instance, reconfigured in place, so its identity stays
  // stable across renders and the chat runtime is never needlessly remounted.
  const transportRef = useRef<ServerAssistantTransport | null>(null);
  if (!transportRef.current) {
    transportRef.current = new ServerAssistantTransport({
      client,
      assistantId: "",
      projectSlug,
      getCorrelationId,
    });
  }

  const [assistantId, setAssistantId] = useState<string>("");
  const [error, setError] = useState<string | null>(null);

  // Resolve the managed assistant the first time the sidebar opens.
  const requestedRef = useRef(false);
  useEffect(() => {
    if (!enabled || requestedRef.current || !projectSlug) return;
    requestedRef.current = true;
    ensureManaged.mutate(
      {},
      {
        onSuccess: (assistant) => setAssistantId(assistant.id),
        onError: () =>
          setError(
            "Couldn't connect to the Project Assistant. Try reopening the sidebar.",
          ),
      },
    );
  }, [enabled, projectSlug, ensureManaged]);

  // Push the latest config into the stable transport instance.
  useEffect(() => {
    transportRef.current?.updateConfig({ client, assistantId, projectSlug });
  }, [client, assistantId, projectSlug]);

  const startFresh = useCallback(() => {
    const fresh = crypto.randomUUID();
    correlationRef.current = fresh;
    try {
      localStorage.setItem(correlationStorageKey(projectSlug), fresh);
    } catch {
      // Ephemeral for this session if persistence fails.
    }
  }, [projectSlug]);

  const ready = assistantId !== "";

  return useMemo(
    () => ({
      transport: ready ? (transportRef.current ?? undefined) : undefined,
      ready,
      error,
      startFresh,
    }),
    [ready, error, startFresh],
  );
}
