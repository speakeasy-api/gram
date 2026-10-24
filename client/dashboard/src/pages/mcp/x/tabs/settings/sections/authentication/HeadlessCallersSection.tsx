import { Button } from "@/components/ui/Button";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Text } from "@/components/ui/Text";
import { clientTypeLabel } from "@/lib/auth-clients";
import type { AuthClient } from "@gram/client/models/components/authclient.js";
import type { IssuanceProfile } from "@gram/client/models/components/issuanceprofile.js";
import {
  invalidateAllIssuanceProfiles,
  useIssuanceProfiles,
} from "@gram/client/react-query/issuanceProfiles.js";
import { useSetIssuanceProfileMutation } from "@gram/client/react-query/setIssuanceProfile.js";
import { useUserSessionClients } from "@gram/client/react-query/userSessionClients.js";
import { useWorkloadIdentities } from "@gram/client/react-query/workloadIdentities.js";
import { useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { AuthRow } from "./AuthRow";

/** A caller that signs in without a browser, as the profile names it. */
type Caller =
  | { kind: "client"; clientId: string; label: string }
  | { kind: "workload"; issuerId: string; subject: string; label: string };

const CLIENT_PREFIX = "client:";
const WORKLOAD_PREFIX = "workload:";

function callerKey(caller: Caller): string {
  return caller.kind === "client"
    ? `${CLIENT_PREFIX}${caller.clientId}`
    : `${WORKLOAD_PREFIX}${caller.issuerId}:${caller.subject}`;
}

function profileKey(profile: IssuanceProfile): string {
  return profile.userSessionClientId
    ? `${CLIENT_PREFIX}${profile.userSessionClientId}`
    : `${WORKLOAD_PREFIX}${profile.workloadIssuerId}:${profile.workloadSubject}`;
}

function isShared(clientType: string): boolean {
  return (
    clientType === "service_account" ||
    clientType === "static_credential" ||
    clientType === "pass_through"
  );
}

/**
 * Which credential the apps and workloads that sign in without a browser use
 * on this server. They cannot pick one on the consent screen, so an admin
 * picks it here once; a token request may still name its own.
 */
export function HeadlessCallersSection({
  mcpServerId,
  userSessionIssuerId,
  credentials,
  canManage,
}: {
  mcpServerId: string;
  userSessionIssuerId: string;
  credentials: AuthClient[];
  canManage: boolean;
}): JSX.Element | null {
  const queryClient = useQueryClient();
  const profiles = useIssuanceProfiles({ userSessionIssuerId });
  const clients = useUserSessionClients({ userSessionIssuerId }, undefined, {
    throwOnError: false,
    retry: false,
  });
  const workloads = useWorkloadIdentities({}, undefined, {
    throwOnError: false,
    retry: false,
  });
  const [adding, setAdding] = useState(false);
  const [callerChoice, setCallerChoice] = useState("");
  const [credentialChoice, setCredentialChoice] = useState("");
  const save = useSetIssuanceProfileMutation({
    onSuccess: () => {
      setAdding(false);
      setCallerChoice("");
      setCredentialChoice("");
      toast.success("Saved");
      void invalidateAllIssuanceProfiles(queryClient);
    },
    onError: (error) =>
      toast.error(error.message || "The change could not be saved. Try again."),
  });

  const callers: Caller[] = [
    ...(clients.data?.result.items ?? []).map((client): Caller => ({
      kind: "client",
      clientId: client.id,
      label: client.clientName,
    })),
    ...(workloads.data?.admissions ?? [])
      .filter((admission) => admission.matchKind === "exact")
      .map((admission): Caller => ({
        kind: "workload",
        issuerId: admission.workloadIssuerId,
        subject: admission.subject,
        label: `${admission.issuerName} · ${admission.subject}`,
      })),
  ];
  const callerLabels = new Map(callers.map((c) => [callerKey(c), c.label]));
  const credentialNames = new Map(credentials.map((c) => [c.id, c.name]));
  const configured = (profiles.data?.items ?? []).flatMap((profile) => {
    const choice = profile.choices.find((c) => c.mcpServerId === mcpServerId);
    return choice ? [{ profile, choice }] : [];
  });
  const selectedCaller = callers.find((c) => callerKey(c) === callerChoice);
  const offered = credentials.filter(
    (credential) =>
      credential.enabled &&
      (selectedCaller?.kind !== "workload" || isShared(credential.clientType)),
  );

  const write = (
    caller: Caller,
    existing: IssuanceProfile | undefined,
    authClientId: string | null,
  ) => {
    const others = (existing?.choices ?? []).filter(
      (c) => c.mcpServerId !== mcpServerId,
    );
    save.mutate({
      request: {
        setIssuanceProfileRequestBody: {
          userSessionIssuerId,
          name: existing?.name ?? caller.label.slice(0, 100),
          userSessionClientId:
            caller.kind === "client" ? caller.clientId : undefined,
          workloadIssuerId:
            caller.kind === "workload" ? caller.issuerId : undefined,
          workloadSubject:
            caller.kind === "workload" ? caller.subject : undefined,
          choices: authClientId
            ? [...others, { mcpServerId, authClientId }]
            : others,
        },
      },
    });
  };

  const callerOf = (profile: IssuanceProfile): Caller =>
    profile.userSessionClientId
      ? {
          kind: "client",
          clientId: profile.userSessionClientId,
          label: profile.name,
        }
      : {
          kind: "workload",
          issuerId: profile.workloadIssuerId ?? "",
          subject: profile.workloadSubject ?? "",
          label: profile.name,
        };

  if (credentials.length < 2 && configured.length === 0) return null;

  return (
    <AuthRow
      label="Apps without a browser"
      hint="Clients and workloads that sign in without a browser can't choose a credential on the consent screen. Pick the one each uses here; a token request can still name its own."
    >
      {configured.length === 0 ? (
        <Text muted small className="block">
          None set. A caller with one ready credential uses it; with several,
          its sign-in asks it to name one.
        </Text>
      ) : (
        <ul className="border-border divide-y border">
          {configured.map(({ profile, choice }) => (
            <li
              key={profile.id}
              className="flex items-center justify-between gap-3 px-4 py-3"
            >
              <div className="min-w-0">
                <span className="block truncate text-sm font-medium">
                  {callerLabels.get(profileKey(profile)) ?? profile.name}
                </span>
                <Text muted small className="block truncate">
                  {choice.authClientId
                    ? (credentialNames.get(choice.authClientId) ??
                      "A credential this server no longer offers")
                    : "No upstream sign-in"}
                </Text>
              </div>
              {canManage ? (
                <Button
                  variant="tertiary"
                  size="sm"
                  aria-label={`Remove the choice for ${profile.name}`}
                  disabled={save.isPending}
                  onClick={() => write(callerOf(profile), profile, null)}
                >
                  <Trash2 className="h-4 w-4" />
                </Button>
              ) : null}
            </li>
          ))}
        </ul>
      )}
      {canManage ? (
        <div className="mt-3">
          <Button variant="secondary" size="sm" onClick={() => setAdding(true)}>
            <Button.LeftIcon>
              <Plus className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>Choose for an app</Button.Text>
          </Button>
        </div>
      ) : null}
      <ConfirmDialog
        open={adding}
        onOpenChange={setAdding}
        title="Choose the credential an app uses"
        description="Sessions it gets from now on use this credential on this server. Sessions it already has keep theirs."
        confirmLabel="Save"
        confirmVariant="primary"
        isPending={save.isPending}
        confirmDisabled={!selectedCaller || !credentialChoice}
        onConfirm={() => {
          if (!selectedCaller) return;
          const existing = (profiles.data?.items ?? []).find(
            (profile) => profileKey(profile) === callerKey(selectedCaller),
          );
          write(selectedCaller, existing, credentialChoice);
        }}
      >
        <div className="space-y-3">
          <Select
            value={callerChoice}
            onValueChange={(value) => {
              setCallerChoice(value);
              setCredentialChoice("");
            }}
          >
            <SelectTrigger aria-label="App" className="w-full">
              <SelectValue placeholder="Choose an app or workload" />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                <SelectLabel>Apps that sign in here</SelectLabel>
                {callers
                  .filter((c) => c.kind === "client")
                  .map((c) => (
                    <SelectItem key={callerKey(c)} value={callerKey(c)}>
                      {c.label}
                    </SelectItem>
                  ))}
              </SelectGroup>
              <SelectGroup>
                <SelectLabel>Workloads</SelectLabel>
                {callers
                  .filter((c) => c.kind === "workload")
                  .map((c) => (
                    <SelectItem key={callerKey(c)} value={callerKey(c)}>
                      {c.label}
                    </SelectItem>
                  ))}
              </SelectGroup>
            </SelectContent>
          </Select>
          <Select
            value={credentialChoice}
            onValueChange={setCredentialChoice}
            disabled={!selectedCaller}
          >
            <SelectTrigger aria-label="Credential" className="w-full">
              <SelectValue placeholder="Choose a credential" />
            </SelectTrigger>
            <SelectContent>
              {offered.map((credential) => (
                <SelectItem key={credential.id} value={credential.id}>
                  {credential.name} · {clientTypeLabel(credential.clientType)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {selectedCaller?.kind === "workload" ? (
            <Text muted small className="block">
              A workload acts for nobody, so only shared credentials are
              offered.
            </Text>
          ) : null}
        </div>
      </ConfirmDialog>
    </AuthRow>
  );
}
