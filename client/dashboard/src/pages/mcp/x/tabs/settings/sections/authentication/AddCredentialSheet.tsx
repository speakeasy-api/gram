import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { FieldError } from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/RadioGroup";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Text } from "@/components/ui/Text";
import {
  clientTypeHint,
  clientTypeLabel,
  serverLabel,
} from "@/lib/auth-clients";
import { useAllRemoteSessionClients } from "@/lib/remote-identity";
import { cn } from "@/lib/utils";
import { remoteSessionClientDisplayName } from "@/pages/remote-identity-providers/clientDisplay";
import { useRoutes } from "@/routes";
import type { AuthClient } from "@gram/client/models/components/authclient.js";
import { CreateAuthClientRequestBodyClientType } from "@gram/client/models/components/createauthclientrequestbody.js";
import { useAttachAuthClientToMcpServerMutation } from "@gram/client/react-query/attachAuthClientToMcpServer.js";
import { useAuthClients } from "@gram/client/react-query/authClients.js";
import { useCreateAuthClientMutation } from "@gram/client/react-query/createAuthClient.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import { invalidateAuthClientQueries } from "./authClientQueries";
import { CredentialHeadersEditor } from "./CredentialHeadersEditor";
import {
  headerDraftProblem,
  headerInputs,
  newHeaderDraft,
  type HeaderDraft,
} from "./credentialHeaders";

/** What the administrator is adding: one of the new kinds, or a reuse. */
type Choice =
  | "existing"
  | "service_account"
  | "static_credential"
  | "pass_through"
  | "remote_session";

const NEW_KINDS: Exclude<Choice, "existing">[] = [
  "service_account",
  "static_credential",
  "pass_through",
  "remote_session",
];

const NEW_KIND_TYPE: Record<
  Exclude<Choice, "existing">,
  CreateAuthClientRequestBodyClientType
> = {
  service_account: CreateAuthClientRequestBodyClientType.ServiceAccount,
  static_credential: CreateAuthClientRequestBodyClientType.StaticCredential,
  pass_through: CreateAuthClientRequestBodyClientType.PassThrough,
  remote_session: CreateAuthClientRequestBodyClientType.RemoteSession,
};

/** The registration owner each signed-in kind needs. */
const REGISTRATION_OWNER: Partial<Record<Choice, "self" | "subject">> = {
  service_account: "self",
  remote_session: "subject",
};

function initialHeaders(choice: Choice): HeaderDraft[] {
  switch (choice) {
    case "static_credential":
      return [newHeaderDraft("stored", "Authorization")];
    case "pass_through":
      return [newHeaderDraft("forwarded", "Authorization")];
    default:
      return [];
  }
}

/**
 * Adds another way for callers to reach this server's upstream: a credential
 * another server already uses, or a new one. Reusing keeps one credential, so
 * a later rotation changes every server at once; that is the first option
 * because it is usually what the administrator wants.
 */
export function AddCredentialSheet({
  mcpServerId,
  projectId,
  tunneled,
  onOpenChange,
}: {
  mcpServerId: string;
  projectId: string;
  /** Tunneled servers are sent no credential headers. */
  tunneled: boolean;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const routes = useRoutes();
  const [choice, setChoice] = useState<Choice>("existing");
  const [existingId, setExistingId] = useState("");
  const [name, setName] = useState("");
  const [registrationId, setRegistrationId] = useState("");
  const [shareAcrossProjects, setShareAcrossProjects] = useState(false);
  const [headers, setHeaders] = useState<HeaderDraft[]>([]);

  const reusable = useAuthClients({ limit: 100 });
  const candidates = (reusable.data?.result.items ?? []).filter(
    (client) =>
      !client.servers.some((server) => server.id === mcpServerId) &&
      (!client.projectId || client.projectId === projectId) &&
      !(tunneled && isHeadersOnly(client.clientType)),
  );

  const owner = REGISTRATION_OWNER[choice];
  const registrations = useAllRemoteSessionClients({}, { enabled: !!owner });
  const registrationOptions = registrations.items.filter(
    (registration) => registration.credentialOwner === owner,
  );
  const registration = registrationOptions.find((r) => r.id === registrationId);

  const done = (message: string) => {
    toast.success(message);
    void invalidateAuthClientQueries(queryClient);
    onOpenChange(false);
  };
  const attach = useAttachAuthClientToMcpServerMutation({
    onSuccess: (client) => done(`${client.name} added to this server`),
    onError: (error) => toast.error(readableError(error)),
  });
  const create = useCreateAuthClientMutation({
    onSuccess: (client) => done(`${client.name} added`),
    onError: (error) => toast.error(readableError(error)),
  });
  const pending = attach.isPending || create.isPending;

  const choose = (next: Choice) => {
    setChoice(next);
    setRegistrationId("");
    setHeaders(initialHeaders(next));
  };

  const problem =
    choice === "existing"
      ? existingId
        ? null
        : "Choose a credential."
      : !name.trim()
        ? "Give the credential a name people will recognize."
        : owner && !registration
          ? "Choose the sign-in it uses."
          : headerDraftProblem(NEW_KIND_TYPE[choice], headers);

  const submit = () => {
    if (problem) return;
    if (choice === "existing") {
      attach.mutate({
        request: {
          attachAuthClientToMcpServerRequestBody: {
            id: existingId,
            mcpServerId,
          },
        },
      });
      return;
    }
    // A registration confined to a project makes the credential that
    // project's; otherwise the administrator decides.
    const confinedTo = registration
      ? registration.projectId || undefined
      : shareAcrossProjects
        ? undefined
        : projectId;
    create.mutate({
      request: {
        createAuthClientRequestBody: {
          clientType: NEW_KIND_TYPE[choice],
          name: name.trim(),
          projectId: confinedTo,
          remoteSessionClientId: registration?.id,
          headers: headerInputs(headers),
          mcpServerIds: [mcpServerId],
        },
      },
    });
  };

  return (
    <Sheet open onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-[560px] flex-col sm:max-w-[560px]"
      >
        <SheetHeader>
          <SheetTitle>Add a credential</SheetTitle>
          <SheetDescription>
            Another way for callers to reach this server&apos;s upstream. Nobody
            can use it until you say who can.
          </SheetDescription>
        </SheetHeader>

        <div className="min-h-0 flex-1 space-y-6 overflow-y-auto px-4 pb-4">
          <RadioGroup
            value={choice}
            onValueChange={(next) => choose(next as Choice)}
            className="gap-1.5"
          >
            <ChoiceRow
              value="existing"
              selected={choice}
              label="A credential another server uses"
              description="Share it instead of making a copy. Changing or rotating it later changes every server that uses it."
            />
            {NEW_KINDS.filter((kind) => !(tunneled && isHeadersOnly(kind))).map(
              (kind) => (
                <ChoiceRow
                  key={kind}
                  value={kind}
                  selected={choice}
                  label={`New: ${clientTypeLabel(kind).toLowerCase()}`}
                  description={clientTypeHint(kind)}
                />
              ),
            )}
          </RadioGroup>

          {choice === "existing" ? (
            <section className="space-y-2">
              <Label htmlFor="existing-credential">Credential</Label>
              {reusable.isLoading ? (
                <Text muted small>
                  Loading credentials…
                </Text>
              ) : reusable.isError ? (
                <FieldError>
                  Credentials could not be loaded. Refresh the page to try
                  again.
                </FieldError>
              ) : candidates.length === 0 ? (
                <Text muted small className="block">
                  No other server has a credential this one can use. Add a new
                  one instead.
                </Text>
              ) : (
                <Select value={existingId} onValueChange={setExistingId}>
                  <SelectTrigger id="existing-credential" className="w-full">
                    <SelectValue placeholder="Choose a credential" />
                  </SelectTrigger>
                  <SelectContent>
                    {candidates.map((client) => (
                      <SelectItem key={client.id} value={client.id}>
                        {client.name} · {clientTypeLabel(client.clientType)}
                        {usedOn(client)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            </section>
          ) : (
            <section className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="credential-name">Name</Label>
                <Input
                  id="credential-name"
                  placeholder={
                    choice === "service_account"
                      ? "e.g. Marketing service account"
                      : "e.g. Engineering automation token"
                  }
                  value={name}
                  onChange={setName}
                />
              </div>

              {owner ? (
                <div className="space-y-2">
                  <Label htmlFor="credential-registration">Sign-in</Label>
                  {registrations.isLoading ? (
                    <Text muted small>
                      Loading sign-ins…
                    </Text>
                  ) : registrationOptions.length === 0 ? (
                    <Alert variant="info" inline>
                      {owner === "self"
                        ? "No sign-in that gets its own token is registered yet. "
                        : "No sign-in for people's own accounts is registered yet. "}
                      Register one in{" "}
                      <Link
                        className="font-medium underline underline-offset-2"
                        to={routes.remoteIdentityProviders.href()}
                      >
                        Authorization Servers
                      </Link>
                      , then come back.
                    </Alert>
                  ) : (
                    <Select
                      value={registrationId}
                      onValueChange={setRegistrationId}
                    >
                      <SelectTrigger
                        id="credential-registration"
                        className="w-full"
                      >
                        <SelectValue placeholder="Choose a sign-in" />
                      </SelectTrigger>
                      <SelectContent>
                        {registrationOptions.map((option) => (
                          <SelectItem key={option.id} value={option.id}>
                            {remoteSessionClientDisplayName(option)}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  )}
                </div>
              ) : (
                <div className="flex items-center gap-2">
                  <Checkbox
                    id="credential-share"
                    checked={shareAcrossProjects}
                    onCheckedChange={(checked) =>
                      setShareAcrossProjects(checked === true)
                    }
                  />
                  <Label htmlFor="credential-share">
                    <Text small>Let servers in other projects use it too</Text>
                  </Label>
                </div>
              )}

              {choice === "remote_session" ? null : (
                <div className="space-y-2">
                  <Label>Headers sent upstream</Label>
                  <CredentialHeadersEditor
                    clientType={NEW_KIND_TYPE[choice]}
                    drafts={headers}
                    onChange={setHeaders}
                    readOnly={false}
                  />
                </div>
              )}
            </section>
          )}
        </div>

        <SheetFooter className="flex-row justify-end gap-2 border-t">
          {problem && (choice === "existing" ? existingId : name) ? (
            <Text muted small className="mr-auto self-center">
              {problem}
            </Text>
          ) : null}
          <Button
            variant="secondary"
            onClick={() => onOpenChange(false)}
            disabled={pending}
          >
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button
            variant="primary"
            onClick={submit}
            disabled={pending || !!problem}
          >
            <Button.Text>{pending ? "Adding…" : "Add credential"}</Button.Text>
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}

function ChoiceRow({
  value,
  selected,
  label,
  description,
}: {
  value: Choice;
  selected: Choice;
  label: string;
  description: string;
}): JSX.Element {
  const id = `add-credential-${value}`;
  return (
    <div
      className={cn(
        "border-border flex items-start gap-2 border p-3",
        selected === value && "border-primary bg-primary/5",
      )}
    >
      <RadioGroupItem value={value} id={id} className="mt-1" />
      <Label
        htmlFor={id}
        className="flex-1 cursor-pointer flex-col items-start gap-0"
      >
        <Text small className="font-medium">
          {label}
        </Text>
        <Text small muted className="mt-0.5">
          {description}
        </Text>
      </Label>
    </div>
  );
}

function isHeadersOnly(clientType: string): boolean {
  return clientType === "static_credential" || clientType === "pass_through";
}

function usedOn(client: AuthClient): string {
  if (client.servers.length === 0) return "";
  if (client.servers.length === 1) {
    return ` · Used on ${serverLabel(client.servers[0]!)}`;
  }
  return ` · Used on ${client.servers.length} servers`;
}

/** The server explains refusals in words meant for the administrator. */
function readableError(error: Error): string {
  return error.message || "The credential could not be added. Try again.";
}
