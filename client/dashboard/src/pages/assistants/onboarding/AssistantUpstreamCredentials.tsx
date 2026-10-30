import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Text } from "@/components/ui/Text";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { clientTypeLabel } from "@/lib/auth-clients";
import type { AssistantExecutionProfile } from "@gram/client/models/components/assistantexecutionprofile.js";
import type { ExecutionProfileServer } from "@gram/client/models/components/executionprofileserver.js";
import type { ListExecutionProfilesResult } from "@gram/client/models/components/listexecutionprofilesresult.js";
import {
  invalidateAllAssistantExecutionProfiles,
  queryKeyAssistantExecutionProfiles,
  useAssistantExecutionProfiles,
} from "@gram/client/react-query/assistantExecutionProfiles.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useSetAssistantExecutionProfileChoiceMutation } from "@gram/client/react-query/setAssistantExecutionProfileChoice.js";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Section } from "./PanelSection";

const AUTOMATIC = "automatic";
const NO_AUTH = "no_auth";

const CONTEXT_LABELS: Record<string, string> = {
  user_backed: "When acting for a person",
  userless: "When acting for nobody",
};

const GAP_LABELS: Record<string, string> = {
  selection_required: "No credential chosen yet. Choose one.",
  personal_requires_user:
    "Only a personal account fits, and these turns act for nobody.",
  no_client: "No credential fits yet.",
  setup_incomplete: "Upstream sign-in is not set up for this server.",
  upstream_auth_undecided:
    "Decide on the server whether it needs upstream credentials.",
};

/**
 * The upstream credential an assistant's turns use on each MCP server it
 * reaches, per trigger and per whether the turn acts for a person. Turns
 * already running keep the credential they started with.
 */
export function AssistantUpstreamCredentials({
  assistantId,
  canWrite,
}: {
  assistantId: string;
  /** Whether the viewer may change this assistant's credentials. */
  canWrite: boolean;
}): JSX.Element | null {
  const gramProject = useProjectSlugForRequests();
  const profiles = useAssistantExecutionProfiles(
    { assistantId, gramProject },
    undefined,
    { throwOnError: false, retry: false },
  );
  const servers = useMcpServers({ gramProject }, undefined, {
    throwOnError: false,
  });
  const names = new Map(
    (servers.data?.mcpServers ?? []).map((s) => [s.id, s.name ?? s.slug]),
  );
  const shown = (profiles.data?.profiles ?? []).filter(
    (p) => p.servers.length > 0 && p.executionContext in CONTEXT_LABELS,
  );

  if (profiles.isError) {
    return (
      <Section title="Upstream credentials">
        <Text small muted>
          The upstream credentials could not be loaded. Refresh the page to try
          again.
        </Text>
      </Section>
    );
  }

  return (
    <Section
      title="Upstream credentials"
      isEmpty={!profiles.isLoading && shown.length === 0}
      empty="This assistant reaches no MCP server that signs in upstream."
    >
      <div className="space-y-4">
        <Text small muted className="block">
          Which credential the assistant uses on each server. Changes apply to
          turns that start afterwards.
        </Text>
        {shown.map((profile) => (
          <ProfileCard
            key={`${profile.triggerInstanceId}:${profile.executionContext}`}
            assistantId={assistantId}
            profile={profile}
            serverName={(id) => names.get(id) ?? "MCP server"}
            canWrite={canWrite}
          />
        ))}
      </div>
    </Section>
  );
}

function ProfileCard({
  assistantId,
  profile,
  serverName,
  canWrite,
}: {
  assistantId: string;
  profile: AssistantExecutionProfile;
  serverName: (id: string) => string;
  canWrite: boolean;
}): JSX.Element {
  const gramProject = useProjectSlugForRequests();
  const queryClient = useQueryClient();
  // One save at a time per profile: every server's change names the version
  // it was read at, so a second change waits for the version the first one
  // made.
  const save = useSetAssistantExecutionProfileChoiceMutation({
    onSuccess: async (saved) => {
      toast.success("Saved");
      queryClient.setQueryData<ListExecutionProfilesResult>(
        queryKeyAssistantExecutionProfiles({ assistantId, gramProject }),
        (current) =>
          current && {
            ...current,
            profiles: current.profiles.map((p) =>
              p.triggerInstanceId === saved.triggerInstanceId &&
              p.executionContext === saved.executionContext
                ? saved
                : p,
            ),
          },
      );
      await invalidateAllAssistantExecutionProfiles(queryClient);
    },
    onError: async (error) => {
      toast.error(error.message || "The change could not be saved. Try again.");
      // A conflict means the profile moved on; reload it so a retry uses the
      // current version.
      await invalidateAllAssistantExecutionProfiles(queryClient);
    },
  });
  const choose = (server: ExecutionProfileServer, value: string) => {
    save.mutate({
      request: {
        gramProject,
        setExecutionProfileChoiceRequestBody: {
          assistantId,
          triggerInstanceId: profile.triggerInstanceId,
          executionContext:
            profile.executionContext === "userless"
              ? "userless"
              : "user_backed",
          mcpServerId: server.mcpServerId,
          expectedVersion: profile.version,
          automatic: value === AUTOMATIC ? true : undefined,
          noAuth: value === NO_AUTH ? true : undefined,
          authClientId:
            value !== AUTOMATIC && value !== NO_AUTH ? value : undefined,
        },
      },
    });
  };
  return (
    <div className="border-border border">
      <div className="border-border border-b px-3 py-2">
        <span className="block text-sm font-medium">{profile.triggerName}</span>
        <Text small muted className="block">
          {CONTEXT_LABELS[profile.executionContext] ?? profile.executionContext}
        </Text>
      </div>
      <ul className="divide-border divide-y">
        {profile.servers.map((server) => (
          <ServerChoice
            key={server.mcpServerId}
            server={server}
            name={serverName(server.mcpServerId)}
            disabled={!canWrite || save.isPending}
            onChoose={(value) => choose(server, value)}
          />
        ))}
      </ul>
    </div>
  );
}

function choiceValue(server: ExecutionProfileServer): string {
  if (!server.chosen || server.executionProfileChoiceSource === AUTOMATIC)
    return AUTOMATIC;
  // A recorded choice the server no longer offers shows as not chosen, so the
  // control stays readable and another credential can replace it.
  if (server.authClientId) {
    return server.options.some((o) => o.authClientId === server.authClientId)
      ? server.authClientId
      : AUTOMATIC;
  }
  return server.noAuthAllowed ? NO_AUTH : AUTOMATIC;
}

function ServerChoice({
  server,
  name,
  disabled,
  onChoose,
}: {
  server: ExecutionProfileServer;
  name: string;
  disabled: boolean;
  onChoose: (value: string) => void;
}): JSX.Element {
  const automaticLabel = server.gapReason
    ? "Not chosen"
    : server.executionProfileChoiceSource === AUTOMATIC
      ? `Automatic (${server.authClientId ? (server.options.find((o) => o.authClientId === server.authClientId)?.name ?? "one credential") : "no sign-in"})`
      : "Automatic";

  return (
    <li className="space-y-1.5 px-3 py-2">
      <span className="block truncate text-sm">{name}</span>
      <Select
        value={choiceValue(server)}
        onValueChange={onChoose}
        disabled={disabled}
      >
        <SelectTrigger
          aria-label={`Credential for ${name}`}
          className="h-8 w-full"
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={AUTOMATIC}>{automaticLabel}</SelectItem>
          {server.options.map((option) => (
            <SelectItem key={option.authClientId} value={option.authClientId}>
              {option.name} · {clientTypeLabel(option.clientType)}
            </SelectItem>
          ))}
          {server.noAuthAllowed ? (
            <SelectItem value={NO_AUTH}>No upstream sign-in</SelectItem>
          ) : null}
        </SelectContent>
      </Select>
      {server.gapReason ? (
        <Text small className="text-warning block">
          {GAP_LABELS[server.gapReason] ?? "No credential chosen."}
        </Text>
      ) : null}
    </li>
  );
}
