import { validateMcpServerUrl } from "@/lib/sources";
import {
  useVerifyRemoteMcpUrl,
  type VerifyRemoteMcpUrlState,
} from "@/pages/sources/remote-mcp/useVerifyRemoteMcpUrl";
import type { RemoteMcpServer } from "@gram/client/models/components/remotemcpserver.js";
import { useUpdateRemoteMcpServerMutation } from "@gram/client/react-query/updateRemoteMcpServer.js";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { invalidateRemoteMcpSourceViews } from "./sourceInvalidation";
import { isForbidden, sourceDestinationLock } from "./sourceDestinationLock";

export type UpstreamUrlDraft = {
  draft: string;
  setDraft: (value: string) => void;
  touch: () => void;
  fieldError: string | null | undefined;
  dirty: boolean;
  invalid: boolean;
  pending: boolean;
  /** Why the URL cannot be changed by this caller, or null when it can. */
  lockedReason: string | null;
  verify: VerifyRemoteMcpUrlState;
  save: () => Promise<void>;
};

// Ported from the retired remote source page. The remote's slug is recomputed
// from its URL server-side, but this page is addressed by the mcp_server's
// own slug, so a URL change no longer needs a route replace.
export function useUpstreamUrlDraft(
  remoteMcpServer: RemoteMcpServer,
): UpstreamUrlDraft {
  const initialUrl = remoteMcpServer.url;
  const [draft, setDraft] = useState(initialUrl);
  const [touched, setTouched] = useState(false);

  // When the upstream URL changes (e.g. another tab edited it), reset the
  // local draft so the input reflects the canonical value.
  useEffect(() => {
    setDraft(initialUrl);
    setTouched(false);
  }, [initialUrl]);

  // A locked URL cannot be edited or saved, so it shows the canonical value:
  // a dirty draft there would only block saving the rest of the form (e.g.
  // after the server refuses it). An unknown lock (a failed background
  // refresh) keeps the draft so typing survives it; a refusal discards it, so
  // a later grant does not bring back a URL the user has moved on from.
  const lock = sourceDestinationLock(remoteMcpServer);
  const refused = remoteMcpServer.environmentLinkAuthorized === false;
  useEffect(() => {
    if (!refused) return;
    setDraft(initialUrl);
    setTouched(false);
  }, [refused, initialUrl]);
  const shown = lock.reason === null ? draft : initialUrl;

  const queryClient = useQueryClient();
  const update = useUpdateRemoteMcpServerMutation();
  const verify = useVerifyRemoteMcpUrl(shown);

  const urlError = validateMcpServerUrl(shown);
  const dirty = shown.trim() !== initialUrl;

  return {
    draft: shown,
    setDraft: (value: string): void => {
      setDraft(value);
      setTouched(true);
    },
    touch: (): void => setTouched(true),
    fieldError: (touched ? urlError : null) ?? update.error?.message,
    dirty,
    invalid: urlError !== null,
    pending: update.isPending,
    lockedReason: lock.reason,
    verify,
    save: async (): Promise<void> => {
      setTouched(true);
      if (urlError !== null) throw new Error(urlError);
      if (lock.reason !== null) throw new Error(lock.reason);
      try {
        await update.mutateAsync({
          request: {
            updateServerForm: { id: remoteMcpServer.id, url: shown.trim() },
          },
        });
      } catch (error) {
        // The cached lock said yes; refresh it so the reason appears. A 403
        // is a refusal of this URL, so discard it now rather than wait for
        // the refresh, which may fail and leave the draft to reappear after
        // a later grant. The refusal still reaches the caller.
        if (isForbidden(error)) {
          setDraft(initialUrl);
          void invalidateRemoteMcpSourceViews(queryClient);
        }
        throw error;
      }
      await invalidateRemoteMcpSourceViews(queryClient);
    },
  };
}
