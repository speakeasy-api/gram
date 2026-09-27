import { useCreateRemoteMcpServerHeaderMutation } from "@gram/client/react-query/createRemoteMcpServerHeader.js";
import { useDeleteRemoteMcpServerHeaderMutation } from "@gram/client/react-query/deleteRemoteMcpServerHeader.js";
import {
  invalidateAllRemoteMcpServerHeaders,
  useRemoteMcpServerHeaders,
} from "@gram/client/react-query/remoteMcpServerHeaders.js";
import { useUpdateRemoteMcpServerHeaderMutation } from "@gram/client/react-query/updateRemoteMcpServerHeader.js";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
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
  validateDrafts,
  type HeaderDraft,
  type HeaderDraftError,
} from "./headerDrafts";

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
  readonly addHeader: () => void;
  readonly replaceHeader: (index: number, draft: HeaderDraft) => void;
  readonly removeHeader: (index: number) => void;
  /** Commit the rows. Resolves false when there was nothing to write. */
  readonly save: () => Promise<boolean>;
};

/**
 * The upstream header rows, as an editable draft over the saved list.
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
  identity?: {
    mode: IdentityMode;
    managed: ManagedHeader | null;
    isError: boolean;
  };
  /** Editing is locked — a shared source, a missing scope, an unknown mode. */
  readOnly?: boolean;
  /** Rows to offer when nothing is configured yet, from the MCP catalog. */
  suggestions?: readonly HeaderDraft[];
}): HeaderDraftsState {
  const queryClient = useQueryClient();
  const headersQuery = useRemoteMcpServerHeaders(
    { remoteMcpServerId },
    undefined,
    { enabled: remoteMcpServerId !== "" },
  );

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

  useEffect(() => {
    const previousSynced = syncedRef.current;
    syncedRef.current = initialDrafts;
    setDrafts((current) =>
      draftsEqual(current, previousSynced) ? initialDrafts : current,
    );
  }, [initialDrafts]);

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

  const createHeader = useCreateRemoteMcpServerHeaderMutation();
  const updateHeader = useUpdateRemoteMcpServerHeaderMutation();
  const deleteHeader = useDeleteRemoteMcpServerHeaderMutation();

  const validationError = validateDrafts(drafts, identityMode, managedHeaderId);
  const fieldErrors = headerDraftErrors(drafts, identityMode, managedHeaderId);
  const isDirty = !draftsEqual(drafts, initialDrafts);
  const saving =
    createHeader.isPending || updateHeader.isPending || deleteHeader.isPending;

  const pristineSuggestions =
    suggestionsSeeded && draftsEqual(drafts, [...suggestedDrafts]);

  const save = async (): Promise<boolean> => {
    if (readOnly || identityError || validationError || !isDirty) return false;

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

    for (const draft of baseline) {
      // The Authorization row belongs to the identity section. It is absent
      // from these drafts precisely when identity has just written it, and
      // deleting it here would undo the save that ran moments ago.
      if (!draft.id || draft.id === managedHeaderId) continue;
      if (keptIds.has(draft.id)) continue;
      await deleteHeader.mutateAsync({ request: { id: draft.id } });
    }

    for (const draft of drafts) {
      // Guard on the id first: an unsaved row and an absent managed header are
      // both undefined, and skipping those would never create anything.
      if (draft.id && draft.id === managedHeaderId) continue;
      const fields = headerDraftToWriteFields(draft);
      if (!draft.id) {
        await createHeader.mutateAsync({
          request: {
            createServerHeaderForm: { remoteMcpServerId, ...fields },
          },
        });
        continue;
      }

      const previous = baselineById.get(draft.id);
      if (previous && draftsEqual([draft], [previous])) continue;

      await updateHeader.mutateAsync({
        request: { updateServerHeaderForm: { id: draft.id, ...fields } },
      });
    }

    await invalidateAllRemoteMcpServerHeaders(queryClient, {
      refetchType: "all",
    });
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
    authorization: {
      mode: identityMode,
      managedHeaderId,
      passThroughHeaderId,
      unknown: identityError,
    },
    readOnly,
    isLoading: headersQuery.isLoading,
    isDirty,
    validationError,
    fieldErrors,
    reportErrors: isDirty && !pristineSuggestions,
    saving,
    error: createHeader.error ?? updateHeader.error ?? deleteHeader.error,
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
  };
}
