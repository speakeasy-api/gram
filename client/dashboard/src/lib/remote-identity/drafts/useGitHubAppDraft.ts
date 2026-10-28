import { useState } from "react";
import { useSdkClient } from "@/contexts/Sdk";
import { useAuthClients } from "@gram/client/react-query/authClients.js";
import { toast } from "sonner";
import {
  gitHubAppReady,
  newGitHubAppDraft,
  type GitHubAppDraft,
} from "../components/GitHubAppFields";

export function useGitHubAppDraft(
  serverId: string,
  upstreamUrl: string | undefined,
  name: string,
  enabled: boolean,
) {
  const client = useSdkClient();
  const query = useAuthClients(
    { mcpServerId: serverId, clientType: "service_account", limit: 100 },
    undefined,
    { enabled, throwOnError: false },
  );
  const bound = query.data?.result.items.find((item) => item.githubApp);
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
  const canSave = !!upstreamUrl && gitHubAppReady(value);
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
    loading: enabled && query.isLoading,
    error: enabled && query.isError,
  };
}
