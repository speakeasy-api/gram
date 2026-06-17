import { useEffect, useMemo, useRef, useState } from "react";
import {
  useAssistantsGetManaged,
  useEnsureManagedAssistantMutation,
  useGramContext,
} from "@gram/client/react-query";
import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
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
  /**
   * True when the project has no managed assistant yet and the caller lacks
   * `project:write`. UI should surface "ask an admin to enable this" rather
   * than the connection-error notice — `sendMessage` itself only needs
   * `project:read`, so once an admin provisions it the same viewer can chat.
   */
  needsAdmin: boolean;
}

/**
 * Resolves the project's server-side Project Assistant and exposes a transport
 * factory wired to it. The conversation id, history, and conversation list are
 * owned by Elements' RemoteThreadListAdapter (backed by the chat service), so
 * this hook only resolves the assistant and builds the send transport.
 *
 * Read (`assistantsGetManaged`) is decoupled from write
 * (`ensureManagedAssistant`): viewers with `project:read` reach an existing
 * managed assistant without ever hitting the write-scoped provisioning path.
 * When the assistant is missing, only writers fire ensure; viewers see
 * `needsAdmin` so the caller can show an "ask an admin" notice.
 */
export function useServerAssistantTransport(
  projectSlug: string,
  enabled: boolean,
): UseServerAssistantTransportResult {
  const client = useGramContext();
  const project = useProject();
  const { hasScope, isLoading: rbacLoading } = useRBAC();

  const canCreate = hasScope("project:write", project.id);

  // The fetcher reads the project from the X-Gram-Project header, but react-
  // query only differentiates by query key — pass projectSlug into the request
  // so a project switch invalidates instead of replaying the old project's
  // cached managed-assistant id.
  const getQuery = useAssistantsGetManaged(
    { gramProject: projectSlug },
    undefined,
    {
      enabled: enabled && !!projectSlug,
      retry: false,
      throwOnError: false,
      refetchOnWindowFocus: false,
    },
  );

  const fetched = getQuery.data;
  const fetchedId = fetched?.id ?? "";
  const queryError = getQuery.error as
    | { statusCode?: number }
    | null
    | undefined;
  const is404 = !!queryError && queryError.statusCode === 404;
  const isOtherError = !!queryError && !is404;

  const ensure = useEnsureManagedAssistantMutation();
  const ensureMutate = ensure.mutate;

  const [provisionedId, setProvisionedId] = useState<string>("");
  const [provisionError, setProvisionError] = useState<string | null>(null);

  // Latch invariant: holds the slug whose ensure call has already fired this
  // session, so re-renders after the read settles don't replay it. Reset when
  // the project switches.
  const provisionedForSlugRef = useRef<string | null>(null);
  useEffect(() => {
    if (provisionedForSlugRef.current === null) return;
    if (provisionedForSlugRef.current === projectSlug) return;
    provisionedForSlugRef.current = null;
    setProvisionedId("");
    setProvisionError(null);
  }, [projectSlug]);

  useEffect(() => {
    if (!enabled || !projectSlug) return;
    if (!is404) return;
    if (rbacLoading || !canCreate) return;
    if (provisionedForSlugRef.current === projectSlug) return;
    provisionedForSlugRef.current = projectSlug;
    const slugAtRequest = projectSlug;
    ensureMutate(
      {},
      {
        onSuccess: (assistant) => {
          if (slugAtRequest !== provisionedForSlugRef.current) return;
          setProvisionedId(assistant.id);
        },
        onError: () => {
          if (slugAtRequest !== provisionedForSlugRef.current) return;
          setProvisionError(
            "Couldn't connect to the Project Assistant. Try reopening the sidebar.",
          );
        },
      },
    );
  }, [enabled, projectSlug, is404, canCreate, rbacLoading, ensureMutate]);

  const assistantId = fetchedId || provisionedId;
  const ready = assistantId !== "";
  const needsAdmin = is404 && !rbacLoading && !canCreate;
  const error = isOtherError
    ? "Couldn't connect to the Project Assistant. Try reopening the sidebar."
    : provisionError;

  const transport = useMemo<ElementsTransportFactory | undefined>(() => {
    if (!ready) return undefined;
    return createServerAssistantTransport({ client, assistantId, projectSlug });
  }, [ready, client, assistantId, projectSlug]);

  return { transport, assistantId, ready, error, needsAdmin };
}
