import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import type { Assistant } from "@gram/client/models/components/assistant.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import {
  setAssistantsGetManagedData,
  useAssistantsGetManaged,
} from "@gram/client/react-query/assistantsGetManaged.js";
import { useEnsureManagedAssistantMutation } from "@gram/client/react-query/ensureManagedAssistant.js";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { DEMO_ORG_SLUG } from "@/lib/demo";
import { isNotFoundError } from "@/lib/route-errors";
import { createServerAssistantTransport } from "@/lib/ServerAssistantTransport";
import type { ElementsTransportFactory } from "@/elements";

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
  /** Whether the caller can use assistants in the target project. */
  allowed: boolean;
  /** Connection error message, if resolving the managed assistant failed. */
  error: string | null;
  /** Re-attempts the failed read or provisioning call behind `error`. */
  retry: () => void;
  /**
   * True when the project has no managed assistant yet and the caller lacks
   * `assistant:write`. UI should surface "ask an admin to enable this" rather
   * than the connection-error notice — `sendMessage` itself only needs
   * `assistant:read`, so once an admin provisions it the same viewer can chat.
   */
  needsAdmin: boolean;
}

const UNAVAILABLE_MESSAGE = "Project assistant is unavailable.";

// Several components call this hook at once (the dock plus each chat list).
// Each instance would otherwise fire its own write-scoped ensure on the same
// 404, so in-flight ensures are shared per query client and project. Slugs are
// unique only within an org, so the key includes the organization id.
const inflightEnsures = new WeakMap<
  QueryClient,
  Map<string, Promise<Assistant>>
>();

function ensureManagedAssistantOnce(
  queryClient: QueryClient,
  organizationId: string,
  projectSlug: string,
  ensure: () => Promise<Assistant>,
): Promise<Assistant> {
  let byProject = inflightEnsures.get(queryClient);
  if (!byProject) {
    byProject = new Map();
    inflightEnsures.set(queryClient, byProject);
  }
  const key = `${organizationId}/${projectSlug}`;
  const inflight = byProject.get(key);
  if (inflight) return inflight;

  const request = ensure()
    .then((assistant) => {
      // Seed the shared read so every instance, and every later mount, sees
      // the assistant instead of the cached 404 that would re-fire ensure.
      setAssistantsGetManagedData(
        queryClient,
        [{ gramSession: undefined, gramProject: projectSlug }],
        assistant,
      );
      return assistant;
    })
    .finally(() => {
      byProject.delete(key);
    });
  byProject.set(key, request);
  return request;
}

interface UseServerAssistantTransportOptions {
  getSkillIds?: () => string[];
  onSkillIdsSent?: (skillIds: string[]) => void;
}

/**
 * Resolves the project's server-side Project Assistant and exposes a transport
 * factory wired to it. The conversation id, history, and conversation list are
 * owned by Elements' RemoteThreadListAdapter (backed by the chat service), so
 * this hook only resolves the assistant and builds the send transport.
 *
 * Read (`assistantsGetManaged`) is decoupled from write
 * (`ensureManagedAssistant`): viewers with `assistant:read` reach an existing
 * managed assistant without ever hitting the write-scoped provisioning path.
 * When the assistant is missing, only writers fire ensure; viewers see
 * `needsAdmin` so the caller can show an "ask an admin" notice.
 */
export function useServerAssistantTransport(
  projectSlug: string,
  enabled: boolean,
  options: UseServerAssistantTransportOptions = {},
): UseServerAssistantTransportResult {
  const client = useGramContext();
  const organization = useOrganization();
  const { session } = useSession();
  // The transport outlives any single session value, so it reads through a ref
  // instead of closing over one.
  const sessionRef = useRef(session);
  sessionRef.current = session;
  const { hasScope, isLoading: rbacLoading } = useRBAC();

  // The hook can be called with a projectSlug that differs from the URL-active
  // project (e.g. the org audit logs route picks an arbitrary project from
  // organization.projects). Scope the RBAC check to THAT project's id, not
  // useProject(), so a user with assistant:write on the target — but not on the
  // active one — still gets the writer path.
  const targetProjectId =
    organization.projects.find((p) => p.slug === projectSlug)?.id ?? "";
  // The demo org advertises the full scope set so pages are browsable, but
  // enforcement is read-only — never auto-fire the write-scoped provisioning
  // path there (it would 403 and toast on every page load).
  // Any assistant grant in the project opens the dock; the server authorizes
  // the managed assistant itself.
  const canRead =
    !!targetProjectId && hasScope("assistant:read", undefined, targetProjectId);
  const canCreate =
    !!targetProjectId &&
    organization.slug !== DEMO_ORG_SLUG &&
    hasScope("assistant:write", targetProjectId, targetProjectId);

  // The fetcher reads the project from the X-Gram-Project header, but react-
  // query only differentiates by query key — pass projectSlug into the request
  // so a project switch invalidates instead of replaying the old project's
  // cached managed-assistant id.
  const authorized = enabled && canRead;
  const getQuery = useAssistantsGetManaged(
    { gramProject: projectSlug },
    undefined,
    {
      enabled: authorized,
      retry: false,
      throwOnError: false,
      refetchOnWindowFocus: false,
    },
  );

  const fetchedId = getQuery.data?.id ?? "";
  const is404 = isNotFoundError(getQuery.error);
  const isOtherError = !!getQuery.error && !is404;

  const queryClient = useQueryClient();
  const organizationId = organization.id;
  // The ensure fires on page load, not on a user action, so a failure must not
  // raise the global "Request failed" toast. The dock shows `error` inline
  // with a retry when the user opens it.
  const ensure = useEnsureManagedAssistantMutation({ onError: () => {} });
  const ensureMutateAsync = ensure.mutateAsync;

  const [provisionedId, setProvisionedId] = useState<string>("");
  const [provisionError, setProvisionError] = useState<string | null>(null);
  // Bumped by `retry` to re-run the provisioning effect after a failure.
  const [retryNonce, setRetryNonce] = useState(0);

  // Latch invariant: holds the slug whose ensure call has already fired this
  // session, so re-renders after the read settles don't replay it. Reset when
  // the project switches.
  const provisionedForSlugRef = useRef<string | null>(null);
  const provisionGenerationRef = useRef(0);
  useEffect(() => {
    if (authorized && provisionedForSlugRef.current === projectSlug) return;
    provisionedForSlugRef.current = null;
    provisionGenerationRef.current += 1;
    setProvisionedId("");
    setProvisionError(null);
  }, [projectSlug, authorized]);

  useEffect(() => {
    if (!authorized || !projectSlug) return;
    if (!is404) return;
    if (rbacLoading || !canCreate) return;
    if (provisionedForSlugRef.current === projectSlug) return;
    provisionedForSlugRef.current = projectSlug;
    const slugAtRequest = projectSlug;
    const generationAtRequest = ++provisionGenerationRef.current;
    // gramProject is explicit so the ensure targets the slug the hook was
    // called with, not whatever the SDK client's default header resolves to
    // — the active project may not be the same one (org-scoped routes).
    ensureManagedAssistantOnce(queryClient, organizationId, projectSlug, () =>
      ensureMutateAsync({ request: { gramProject: projectSlug } }),
    ).then(
      (assistant) => {
        if (
          slugAtRequest !== provisionedForSlugRef.current ||
          generationAtRequest !== provisionGenerationRef.current
        )
          return;
        setProvisionedId(assistant.id);
      },
      () => {
        if (
          slugAtRequest !== provisionedForSlugRef.current ||
          generationAtRequest !== provisionGenerationRef.current
        )
          return;
        // Clear the latch so the next dep change (project switch, or a
        // future caller wiring `enabled` to the sidebar toggle again) can
        // re-fire ensure. Without this, a transient 500 or a 409 from the
        // managed-name conflict would stick until full page reload.
        provisionedForSlugRef.current = null;
        setProvisionError(UNAVAILABLE_MESSAGE);
      },
    );
  }, [
    retryNonce,
    authorized,
    organizationId,
    projectSlug,
    is404,
    canCreate,
    rbacLoading,
    queryClient,
    ensureMutateAsync,
  ]);

  const assistantId = authorized
    ? fetchedId ||
      (provisionedForSlugRef.current === projectSlug ? provisionedId : "")
    : "";
  const ready = assistantId !== "";
  const needsAdmin = authorized && is404 && !rbacLoading && !canCreate;
  const error = isOtherError ? UNAVAILABLE_MESSAGE : provisionError;
  const refetchManaged = getQuery.refetch;
  const retry = useCallback(() => {
    if (isOtherError) {
      void refetchManaged();
      return;
    }
    setProvisionError(null);
    setRetryNonce((n) => n + 1);
  }, [isOtherError, refetchManaged]);

  const transport = useMemo<ElementsTransportFactory | undefined>(() => {
    if (!ready) return undefined;
    return createServerAssistantTransport({
      client,
      assistantId,
      projectSlug,
      getSkillIds: options.getSkillIds,
      onSkillIdsSent: options.onSkillIdsSent,
      // The turn stream authenticates from headers, not cookies.
      getSessionToken: () => sessionRef.current,
    });
  }, [
    ready,
    client,
    assistantId,
    projectSlug,
    options.getSkillIds,
    options.onSkillIdsSent,
  ]);

  return {
    transport,
    assistantId,
    ready,
    allowed: canRead,
    error,
    retry,
    needsAdmin,
  };
}
