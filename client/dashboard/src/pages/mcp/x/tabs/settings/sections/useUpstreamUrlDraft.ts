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
import { useSourceDestinationLock } from "./useSourceDestinationLock";

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

  const queryClient = useQueryClient();
  const update = useUpdateRemoteMcpServerMutation();
  const verify = useVerifyRemoteMcpUrl(draft);
  const lock = useSourceDestinationLock({
    projectId: remoteMcpServer.projectId,
    environmentLinked: remoteMcpServer.environmentLinked,
  });

  const urlError = validateMcpServerUrl(draft);
  const dirty = draft.trim() !== initialUrl;

  return {
    draft,
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
      await update.mutateAsync({
        request: {
          updateServerForm: { id: remoteMcpServer.id, url: draft.trim() },
        },
      });
      await invalidateRemoteMcpSourceViews(queryClient);
    },
  };
}
