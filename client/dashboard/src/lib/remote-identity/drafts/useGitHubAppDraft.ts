import type { AuthClient } from "@gram/client/models/components/authclient.js";
import { useState, useEffect, type Dispatch, type SetStateAction } from "react";
import { useSdkClient } from "@/contexts/Sdk";
import { useAuthClientsInfinite } from "@gram/client/react-query/authClients.js";
import { toast } from "sonner";
import {
  gitHubAppReady,
  newGitHubAppDraft,
  type GitHubAppDraft,
} from "../model/githubApp";

type GitHubAppDraftState = {
  value: GitHubAppDraft;
  setDraft: Dispatch<SetStateAction<GitHubAppDraft | null>>;
  bound: AuthClient | undefined;
  saving: boolean;
  canSave: boolean;
  save: () => Promise<boolean>;
  detach: () => Promise<void>;
  loading: boolean;
  error: boolean;
  retry: () => void;
};

export function useGitHubAppDraft(
  serverId: string,
  upstreamUrl: string | undefined,
  name: string,
  enabled: boolean,
): GitHubAppDraftState {
  const client = useSdkClient();
  const query = useAuthClientsInfinite(
    { mcpServerId: serverId, clientType: "service_account", limit: 100 },
    undefined,
    { enabled, throwOnError: false },
  );
  const {
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    fetchNextPage,
  } = query;
  useEffect(() => {
    if (!enabled || !hasNextPage || isFetchingNextPage || isFetchNextPageError)
      return;
    void fetchNextPage().catch(() => undefined);
  }, [
    enabled,
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    fetchNextPage,
  ]);
  const incomplete =
    query.hasNextPage || query.isFetchingNextPage || query.isFetchNextPageError;
  const bound = query.data?.pages
    .flatMap((page) => page.result.items)
    .find((item) => item.githubApp);
  const [draft, setDraft] = useState<GitHubAppDraft | null>(null);
  const [saving, setSaving] = useState(false);
  const value =
    draft ??
    (bound?.githubApp
      ? {
          instanceUrl: bound.githubApp.instanceUrl,
          appId: bound.githubApp.appId,
          installationId: bound.githubApp.installationId,
          privateKey: "",
        }
      : newGitHubAppDraft());
  const canSave =
    !incomplete &&
    !query.isLoading &&
    !query.isError &&
    !!upstreamUrl &&
    gitHubAppReady(value);
  const save = async () => {
    if (!canSave || !upstreamUrl) return false;
    setSaving(true);
    try {
      await client.authClients.configureGitHubApp({
        configureGitHubAppAuthClientRequestBody: {
          ...value,
          id: bound?.id,
          mcpServerId: serverId,
          name: bound?.name ?? `${name} service account`,
          upstreamOrigin: new URL(upstreamUrl).origin,
        },
      });
      setDraft(null);
      await query.refetch();
      toast.success("GitHub App service account saved");
      return true;
    } catch {
      toast.error(
        "Could not verify or save the GitHub App. Check its credentials, installation, and network access.",
      );
      return false;
    } finally {
      setSaving(false);
    }
  };
  const detach = async () => {
    if (!bound) return;
    await client.authClients.detachFromMcpServer({
      detachAuthClientFromMcpServerRequestBody: {
        id: bound.id,
        mcpServerId: serverId,
      },
    });
    setDraft(null);
    await query.refetch();
  };
  return {
    value,
    setDraft,
    bound,
    saving,
    canSave,
    save,
    detach,
    loading: enabled && (query.isLoading || !!incomplete),
    error: enabled && (query.isError || query.isFetchNextPageError),
    retry: () => {
      void query.refetch().catch(() => undefined);
    },
  };
}
