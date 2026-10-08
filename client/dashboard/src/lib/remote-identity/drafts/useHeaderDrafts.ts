import { useCreateRemoteMcpServerHeaderMutation } from "@gram/client/react-query/createRemoteMcpServerHeader.js";
import { useDeleteRemoteMcpServerHeaderMutation } from "@gram/client/react-query/deleteRemoteMcpServerHeader.js";
import {
  invalidateAllRemoteMcpServerHeaders,
  useRemoteMcpServerHeaders,
} from "@gram/client/react-query/remoteMcpServerHeaders.js";
import { useUpdateRemoteMcpServerHeaderMutation } from "@gram/client/react-query/updateRemoteMcpServerHeader.js";
import { useCreateTunneledMcpServerHeaderMutation } from "@gram/client/react-query/createTunneledMcpServerHeader.js";
import { useDeleteTunneledMcpServerHeaderMutation } from "@gram/client/react-query/deleteTunneledMcpServerHeader.js";
import {
  invalidateAllTunneledMcpServerHeaders,
  useTunneledMcpServerHeaders,
} from "@gram/client/react-query/tunneledMcpServerHeaders.js";
import { useUpdateTunneledMcpServerHeaderMutation } from "@gram/client/react-query/updateTunneledMcpServerHeader.js";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { toError } from "@/lib/errors";
import type { IdentityMode } from "../model/identity";
import {
  findPassThroughAuthorizationHeader,
  type ManagedHeader,
} from "../model/headers";
import {
  draftsEqual,
  headerDraftErrors,
  headerDraftFromServer,
  headerDraftToWriteFields,
  newHeaderDraft,
  type HeaderWriteFields,
  validateDrafts,
  type HeaderDraft,
  type HeaderDraftError,
  type HeaderPolicy,
  type ServerHeader,
} from "./headerDrafts";

/** The saved header list, from whichever kind of source the rows edit. */
type HeadersQuery = {
  readonly data?: { readonly headers?: readonly ServerHeader[] };
  readonly isLoading: boolean;
  readonly isError: boolean;
  readonly refetch: () => Promise<{
    readonly isError: boolean;
    readonly data?: { readonly headers?: readonly ServerHeader[] };
  }>;
};

/** How the rows are written to one source. */
type HeaderWrites = {
  /** Resolves with the saved row, secret values redacted. */
  readonly create: (fields: HeaderWriteFields) => Promise<ServerHeader>;
  /** Resolves with the saved row, secret values redacted. */
  readonly update: (
    id: string,
    fields: HeaderWriteFields,
  ) => Promise<ServerHeader>;
  readonly remove: (id: string) => Promise<unknown>;
  readonly invalidate: () => Promise<unknown>;
  /** Drops every mutation's retained request, which holds submitted secrets. */
  readonly reset: () => void;
  readonly isPending: boolean;
};

/**
 * What the identity panel says about the Authorization row, resolved once so
 * the rows never re-derive it from a second fetch.
 */
type HeaderAuthorizationContext = {
  /** The mode these rows are validated against, when it is known. */
  readonly mode?: IdentityMode;
  /** The row identity owns: shown, never edited here. */
  readonly managedHeaderId?: string;
  /** A legacy pass-through Authorization row, which should be removed. */
  readonly passThroughHeaderId?: string;
  /** The identity answer is unavailable, so editing is locked. */
  readonly unknown: boolean;
};

export type HeaderDraftsState = {
  readonly drafts: HeaderDraft[];
  readonly authorization: HeaderAuthorizationContext;
  /** Editing is locked: a shared source, a missing scope, an unknown mode. */
  readonly readOnly: boolean;
  readonly isLoading: boolean;
  /** The rows differ from what the server holds. */
  readonly isDirty: boolean;
  /** Why the rows cannot be written yet, or null. */
  readonly validationError: string | null;
  /** The problem on each row, keyed by draft key, so a field can mark itself. */
  readonly fieldErrors: ReadonlyMap<string, HeaderDraftError>;
  /**
   * Whether to say any of that out loud yet. Freshly seeded catalog rows are
   * value-less on purpose: they hold Save closed, but pointing at them before
   * the operator has typed anything is scolding them for our own suggestion.
   */
  readonly reportErrors: boolean;
  readonly saving: boolean;
  readonly error: Error | null;
  /** The headers could not be loaded, so editing is locked. */
  readonly loadError: boolean;
  readonly addHeader: () => void;
  readonly replaceHeader: (index: number, draft: HeaderDraft) => void;
  readonly removeHeader: (index: number) => void;
  /** Commit the rows. Resolves false when there was nothing to write. */
  readonly save: () => Promise<boolean>;
  /** Drop every unsaved edit and return to what the server holds. */
  readonly discard: () => void;
};

/**
 * The upstream header rows of a remote source, as an editable draft over the
 * saved list.
 *
 * The state lives here rather than in the section that renders it because the
 * identity panel commits headers and identity together: the footer's Save has
 * to know whether the rows are dirty and whether they are writable, which it
 * cannot ask of a component's private state.
 */
export function useHeaderDrafts({
  remoteMcpServerId,
  identity,
  readOnly = false,
  suggestions,
}: {
  remoteMcpServerId: string;
  /**
   * The identity panel's answer, when there is one. It owns the Authorization
   * row, so these rows neither validate it nor write it.
   */
  identity?: HeaderIdentity;
  /** Editing is locked — a shared source, a missing scope, an unknown mode. */
  readOnly?: boolean;
  /** Rows to offer when nothing is configured yet, from the MCP catalog. */
  suggestions?: readonly HeaderDraft[];
}): HeaderDraftsState {
  const queryClient = useQueryClient();
  const headersQuery = useRemoteMcpServerHeaders(
    { remoteMcpServerId },
    undefined,
    { enabled: remoteMcpServerId !== "", throwOnError: false },
  );
  const createHeader = useCreateRemoteMcpServerHeaderMutation();
  const updateHeader = useUpdateRemoteMcpServerHeaderMutation();
  const deleteHeader = useDeleteRemoteMcpServerHeaderMutation();

  return useHeaderDraftsFor({
    policy: "remote",
    headersQuery,
    writes: {
      create: (fields) =>
        createHeader.mutateAsync({
          request: {
            createServerHeaderForm: { remoteMcpServerId, ...fields },
          },
        }),
      update: (id, fields) =>
        updateHeader.mutateAsync({
          request: { updateServerHeaderForm: { id, ...fields } },
        }),
      remove: (id) => deleteHeader.mutateAsync({ request: { id } }),
      invalidate: () =>
        invalidateAllRemoteMcpServerHeaders(queryClient, {
          refetchType: "all",
        }),
      reset: () => {
        createHeader.reset();
        updateHeader.reset();
        deleteHeader.reset();
      },
      isPending:
        createHeader.isPending ||
        updateHeader.isPending ||
        deleteHeader.isPending,
    },
    identity,
    readOnly,
    suggestions,
  });
}

/**
 * The header rows of a tunneled source. They are stored on the tunnel, so
 * every MCP server on it sends them, and they are checked against the
 * tunnel's stricter rules.
 *
 * The rows belong to one tunnel for the life of the component: callers key the
 * component by the tunnel, so moving to another remounts it and no unsaved
 * row, secret included, carries across, while a save in flight finishes
 * against the tunnel it started on.
 */
export function useTunneledHeaderDrafts({
  tunneledMcpServerId,
  readOnly = false,
}: {
  tunneledMcpServerId: string;
  /** Editing is locked, for instance by a missing scope. */
  readOnly?: boolean;
}): HeaderDraftsState {
  const queryClient = useQueryClient();
  const headersQuery = useTunneledMcpServerHeaders(
    { tunneledMcpServerId },
    undefined,
    { enabled: tunneledMcpServerId !== "", throwOnError: false },
  );
  const createHeader = useCreateTunneledMcpServerHeaderMutation();
  const updateHeader = useUpdateTunneledMcpServerHeaderMutation();
  const deleteHeader = useDeleteTunneledMcpServerHeaderMutation();

  return useHeaderDraftsFor({
    policy: "tunneled",
    headersQuery,
    writes: {
      create: (fields) =>
        createHeader.mutateAsync({
          request: {
            createTunneledMcpServerHeaderForm: {
              tunneledMcpServerId,
              ...fields,
            },
          },
        }),
      update: (id, fields) =>
        updateHeader.mutateAsync({
          request: { updateTunneledMcpServerHeaderForm: { id, ...fields } },
        }),
      remove: (id) => deleteHeader.mutateAsync({ request: { id } }),
      invalidate: () =>
        invalidateAllTunneledMcpServerHeaders(queryClient, {
          refetchType: "all",
        }),
      reset: () => {
        createHeader.reset();
        updateHeader.reset();
        deleteHeader.reset();
      },
      isPending:
        createHeader.isPending ||
        updateHeader.isPending ||
        deleteHeader.isPending,
    },
    identity: undefined,
    readOnly,
    suggestions: undefined,
  });
}

type HeaderIdentity = {
  mode: IdentityMode;
  managed: ManagedHeader | null;
  isError: boolean;
};

function useHeaderDraftsFor({
  policy,
  headersQuery,
  writes,
  identity,
  readOnly,
  suggestions,
}: {
  policy: HeaderPolicy;
  headersQuery: HeadersQuery;
  writes: HeaderWrites;
  identity: HeaderIdentity | undefined;
  readOnly: boolean;
  suggestions: readonly HeaderDraft[] | undefined;
}): HeaderDraftsState {
  const identityError =
    !!identity && (identity.isError || headersQuery.isError);
  const identityMode = identity && !identityError ? identity.mode : undefined;
  const managedHeaderId = identity?.managed?.headerId ?? undefined;
  const passThroughHeaderId = identity
    ? findPassThroughAuthorizationHeader(headersQuery.data?.headers ?? [])?.id
    : undefined;

  const initialDrafts = useMemo(
    () => (headersQuery.data?.headers ?? []).map(headerDraftFromServer),
    [headersQuery.data],
  );
  const [drafts, setDrafts] = useState(initialDrafts);
  // The server snapshot the current drafts were last synced from. Used to tell
  // "nobody has touched anything" apart from "there are unsaved edits" so a
  // background refetch (window refocus, concurrent change) can't silently
  // discard work in progress.
  const syncedRef = useRef(initialDrafts);
  // The server snapshot last adopted into the rows. Only a newer one is
  // adopted, so a save whose rows were reconciled write by write is not
  // reverted to the stale pre-save snapshot when its own refresh fails.
  const adoptedRef = useRef(initialDrafts);

  useEffect(() => {
    // Advance the baseline only when there is nothing to lose. Save measures
    // its deletions against this snapshot, so moving it under a dirty form
    // would make a row that appeared since — someone else's concurrent add —
    // look like a row the operator deleted, and the diff would remove it.
    // A save reconciles rows one write at a time; the query still holds the
    // pre-save list until the save's own refresh lands.
    if (committingRef.current) return;
    if (initialDrafts === adoptedRef.current) return;
    if (!draftsEqual(drafts, syncedRef.current)) return;
    adoptedRef.current = initialDrafts;
    syncedRef.current = initialDrafts;
    setDrafts(initialDrafts);
  }, [drafts, initialDrafts]);

  // Seed suggested rows only into a form that has nothing in it, and only
  // once: re-seeding would resurrect rows the operator deleted on purpose.
  const suggestedDrafts = useMemo(() => suggestions ?? [], [suggestions]);
  const [suggestionsSeeded, setSuggestionsSeeded] = useState(false);
  useEffect(() => {
    if (readOnly || suggestionsSeeded) return;
    if (!headersQuery.data || initialDrafts.length > 0) return;
    if (suggestedDrafts.length === 0) return;
    setSuggestionsSeeded(true);
    setDrafts((current) =>
      current.length === 0 ? [...suggestedDrafts] : current,
    );
  }, [
    readOnly,
    suggestionsSeeded,
    suggestedDrafts,
    headersQuery.data,
    initialDrafts,
  ]);

  // Held here rather than read off the mutations: those are reset after every
  // write so they stop holding the submitted secret, and reset clears their
  // error too.
  const [writeError, setWriteError] = useState<Error | null>(null);

  const validationError = validateDrafts(
    drafts,
    identityMode,
    managedHeaderId,
    policy,
  );
  const fieldErrors = headerDraftErrors(
    drafts,
    identityMode,
    managedHeaderId,
    policy,
  );
  const isDirty = !draftsEqual(drafts, initialDrafts);
  const writing = writes.isPending;
  // A failed load must not open an empty form: saving it would read as
  // deleting every header the source really has.
  const loadError = headersQuery.isError;
  const locked = readOnly || loadError;

  const pristineSuggestions =
    suggestionsSeeded && draftsEqual(drafts, [...suggestedDrafts]);

  // The commit ends with a refetch that replaces every row, and the writes
  // settle (and are reset) before it lands. Held across the whole commit so the
  // rows and Save stay shut until then: an edit typed meanwhile would be
  // overwritten, and a second Save would recreate rows that still lack ids.
  const [committing, setCommitting] = useState(false);
  const committingRef = useRef(false);
  const saving = writing || committing;

  const save = async (): Promise<boolean> => {
    if (locked || identityError || validationError || !isDirty) return false;
    if (committingRef.current) return false;
    committingRef.current = true;
    setCommitting(true);
    try {
      return await commit();
    } finally {
      committingRef.current = false;
      setCommitting(false);
    }
  };

  const commit = async (): Promise<boolean> => {
    // Diff against what the server actually holds now, not the snapshot this
    // render was built from: identity commits first and refetches headers, so
    // the query may have moved underneath us.
    const baseline = syncedRef.current;
    const baselineById = new Map(
      baseline
        .filter((draft): draft is HeaderDraft & { id: string } => !!draft.id)
        .map((draft) => [draft.id, draft]),
    );
    const keptIds = new Set(
      drafts.flatMap((draft) => (draft.id ? [draft.id] : [])),
    );

    // Each acknowledged write is folded into the baseline and the rows as it
    // lands, so a save that fails part way leaves the form describing exactly
    // what is still unsaved: a retry neither repeats a delete (which the
    // server now answers with not found) nor recreates a row it already made.
    const adopt = (key: string, saved: ServerHeader) => {
      const adopted = headerDraftFromServer(saved);
      syncedRef.current = [
        ...syncedRef.current.filter((row) => row.id !== adopted.id),
        adopted,
      ];
      setDrafts((current) =>
        current.map((row) => (row.key === key ? adopted : row)),
      );
    };
    let committed = false;

    setWriteError(null);
    try {
      for (const draft of baseline) {
        // The Authorization row belongs to the identity section. It is absent
        // from these drafts precisely when identity has just written it, and
        // deleting it here would undo the save that ran moments ago.
        if (!draft.id || draft.id === managedHeaderId) continue;
        if (keptIds.has(draft.id)) continue;
        await writes.remove(draft.id);
        committed = true;
        syncedRef.current = syncedRef.current.filter(
          (row) => row.id !== draft.id,
        );
      }

      for (const draft of drafts) {
        // Guard on the id first: an unsaved row and an absent managed header are
        // both undefined, and skipping those would never create anything.
        if (draft.id && draft.id === managedHeaderId) continue;
        const fields = headerDraftToWriteFields(draft);
        if (!draft.id) {
          adopt(draft.key, await writes.create(fields));
          committed = true;
          continue;
        }

        const previous = baselineById.get(draft.id);
        if (previous && draftsEqual([draft], [previous])) continue;

        adopt(draft.key, await writes.update(draft.id, fields));
        committed = true;
      }
    } catch (error) {
      setWriteError(toError(error));
      throw error;
    } finally {
      // react-query keeps a settled mutation's request variables, and for
      // these that means the plaintext secret stays readable in client state
      // long after the write — failed writes included. Nothing reads them
      // again, so drop them.
      writes.reset();
      // Whatever did land is real, so every other reader of these headers
      // must see it even when the save as a whole failed.
      if (committed) await writes.invalidate();
    }

    // Adopt the canonical server state so the rows pick up server-assigned ids
    // and secret redaction. The sync effect preserves unsaved edits, so this
    // reset has to be explicit.
    const refreshed = await headersQuery.refetch();
    if (refreshed.isError || !refreshed.data) {
      // The write landed but the refresh did not. Treating the missing result
      // as an empty header list would wipe the form, so leave the rows alone
      // and say so.
      toast.warning("Headers saved, but the list could not be refreshed.");
      return true;
    }
    const synced = (refreshed.data.headers ?? []).map(headerDraftFromServer);
    syncedRef.current = synced;
    setDrafts(synced);
    return true;
  };

  return {
    drafts,
    // The commit ends with a refetch that replaces every row, so editing has
    // to stay shut until that lands or the new snapshot overwrites whatever
    // was typed in the meantime.
    authorization: {
      mode: identityMode,
      managedHeaderId,
      passThroughHeaderId,
      unknown: identityError,
    },
    readOnly: locked || saving,
    isLoading: headersQuery.isLoading,
    isDirty,
    validationError,
    fieldErrors,
    reportErrors: isDirty && !pristineSuggestions,
    saving,
    error: writeError,
    loadError,
    addHeader: () => setDrafts((current) => [...current, newHeaderDraft()]),
    replaceHeader: (index, draft) =>
      setDrafts((current) =>
        current.map((row, rowIndex) => (rowIndex === index ? draft : row)),
      ),
    removeHeader: (index) =>
      setDrafts((current) =>
        current.filter((_, rowIndex) => rowIndex !== index),
      ),
    save,
    discard: () => setDrafts(syncedRef.current),
  };
}
