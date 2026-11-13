import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Combobox } from "@/components/ui/Combobox";
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
import { useAllAuthClients } from "@/lib/useAllAuthClients";
import { IssuerLogo } from "@/lib/remote-identity";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import type { AuthClient } from "@gram/client/models/components/authclient.js";
import { CreateAuthClientRequestBodyClientType } from "@gram/client/models/components/createauthclientrequestbody.js";
import { useAttachAuthClientToMcpServerMutation } from "@gram/client/react-query/attachAuthClientToMcpServer.js";
import { useCreateAuthClientMutation } from "@gram/client/react-query/createAuthClient.js";
import { useSdkClient } from "@/contexts/Sdk";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { useQueryClient } from "@tanstack/react-query";
import { useRef, useState } from "react";
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
import { PersonalSignInFields } from "./PersonalSignInFields";
import { usePersonalSignInDraft } from "./usePersonalSignInDraft";
import { useSignInChoices } from "./useSignInChoices";

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

/**
 * The kinds that pick a stored registration to authenticate through. A
 * personal sign-in is not one of them: it always gets a registration of its
 * own, because a person holds one upstream grant per registration.
 */
const REGISTRATION_OWNER: Partial<Record<Choice, "self">> = {
  service_account: "self",
};

function initialHeaders(choice: Choice): HeaderDraft[] {
  switch (choice) {
    case "static_credential":
      return [newHeaderDraft("stored", "Authorization")];
    case "pass_through":
      return [newHeaderDraft("forwarded", "Authorization")];
    case "existing":
    case "service_account":
    case "remote_session":
      return [];
  }
}

/**
 * Adds another way for callers to reach this server's upstream: a credential
 * another server already uses, or a new one. Reusing keeps one credential, so
 * a later rotation changes every server at once; that is the first option
 * because it is usually what the administrator wants.
 *
 * Sign-ins for people's own accounts are the exception and are never reused:
 * a person holds one upstream grant per sign-in, so a second server on the
 * same one could replace the grant the first obtained. Each gets a sign-in of
 * its own, from scratch or started from a stored one's settings.
 */
export function AddCredentialSheet({
  mcpServerId,
  projectId,
  remoteMcpServerId,
  tunneled,
  onOpenChange,
}: {
  mcpServerId: string;
  projectId: string;
  /** Lets the sign-in list narrow to the upstream's authorization servers. */
  remoteMcpServerId: string | undefined;
  /** Tunneled servers are sent no credential headers. */
  tunneled: boolean;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const routes = useRoutes();
  const sdk = useSdkClient();
  const [picked, setPicked] = useState<Choice | null>(null);
  const [existingId, setExistingId] = useState("");
  const [name, setName] = useState("");
  const [registrationId, setRegistrationId] = useState("");
  const [shareAcrossProjects, setShareAcrossProjects] = useState(false);
  const [headers, setHeaders] = useState<HeaderDraft[]>([]);

  const reusable = useAllAuthClients();
  const candidates = (reusable.data ?? []).filter(
    (client) =>
      client.clientType !== "remote_session" &&
      !client.servers.some((server) => server.id === mcpServerId) &&
      (!client.projectId || client.projectId === projectId) &&
      !(tunneled && isHeadersOnly(client.clientType)),
  );
  // Default to creation only before an explicit choice. A refresh must not
  // turn an existing-credential selection into a request to create one.
  const offersExisting =
    picked === "existing" || !reusable.isSuccess || candidates.length > 0;
  const choice: Choice =
    picked ?? (offersExisting ? "existing" : "service_account");

  const owner = REGISTRATION_OWNER[choice];
  const signIns = useSignInChoices(owner, remoteMcpServerId);
  const signIn = signIns.choices.find(
    (candidate) => candidate.registration.id === registrationId,
  );
  const registration = signIn?.registration;
  const personal = usePersonalSignInDraft({
    remoteMcpServerId,
    enabled: choice === "remote_session",
  });
  // A sign-in made for an attempt whose credential then failed to save. A
  // retry of the same form reuses it; anything else discards it, so a failed
  // save leaves nothing behind.
  const madeSignIn = useRef<{
    signature: string;
    client: RemoteSessionClient;
  } | null>(null);
  const discardMadeSignIn = () => {
    const made = madeSignIn.current;
    madeSignIn.current = null;
    if (!made) return;
    void sdk.remoteSessionClients
      .delete({ id: made.client.id })
      .catch(() => undefined);
  };

  // A choice the narrower list leaves out must not come back later unseen.
  const toggleShowAll = () => {
    if (signIns.showAll && !signIns.matching.some((c) => c === signIn)) {
      setRegistrationId("");
    }
    signIns.setShowAll(!signIns.showAll);
  };

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
  const [makingSignIn, setMakingSignIn] = useState(false);
  const pending = attach.isPending || create.isPending || makingSignIn;

  const close = () => {
    // A save in flight may be about to use the sign-in made for it.
    if (pending) return;
    discardMadeSignIn();
    onOpenChange(false);
  };

  const choose = (next: Choice) => {
    setPicked(next);
    setRegistrationId("");
    signIns.setShowAll(false);
    setHeaders(initialHeaders(next));
  };

  const problem =
    choice === "existing"
      ? !existingId
        ? "Choose a credential."
        : candidates.some((client) => client.id === existingId)
          ? null
          : "This credential is no longer available. Choose another credential."
      : !name.trim()
        ? "Give the credential a name people will recognize."
        : owner && !registration
          ? "Choose the sign-in it uses."
          : choice === "remote_session"
            ? personal.problem
            : headerDraftProblem(NEW_KIND_TYPE[choice], headers);

  const submitPersonal = async () => {
    setMakingSignIn(true);
    let signIn: RemoteSessionClient;
    try {
      if (madeSignIn.current?.signature !== personal.signature) {
        discardMadeSignIn();
        madeSignIn.current = {
          signature: personal.signature,
          client: await personal.create(),
        };
        void invalidateAllRemoteSessionClients(queryClient);
      }
      signIn = madeSignIn.current.client;
    } catch (error) {
      toast.error(
        readableError(error instanceof Error ? error : new Error("")),
      );
      return;
    } finally {
      setMakingSignIn(false);
    }
    create.mutate(
      {
        request: {
          createAuthClientRequestBody: {
            clientType: NEW_KIND_TYPE.remote_session,
            name: name.trim(),
            projectId: signIn.projectId || undefined,
            remoteSessionClientId: signIn.id,
            headers: [],
            mcpServerIds: [mcpServerId],
          },
        },
      },
      {
        // The credential owns the sign-in now.
        onSuccess: () => {
          madeSignIn.current = null;
        },
      },
    );
  };

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
    if (choice === "remote_session") {
      void submitPersonal();
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
    <Sheet open onOpenChange={(open) => (open ? onOpenChange(true) : close())}>
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
            {offersExisting ? (
              <ChoiceRow
                value="existing"
                selected={choice}
                label="Existing credential"
                description="Share a service account or stored credential another server uses instead of making a copy. Changing or rotating it later changes every server that uses it. Sign-ins for people's own accounts are not shared: add one below."
              />
            ) : null}
            {NEW_KINDS.filter((kind) => !(tunneled && isHeadersOnly(kind))).map(
              (kind) => (
                <ChoiceRow
                  key={kind}
                  value={kind}
                  selected={choice}
                  label={clientTypeLabel(kind)}
                  description={clientTypeHint(kind)}
                />
              ),
            )}
          </RadioGroup>

          {choice === "existing" ? (
            <section className="space-y-2">
              <Label htmlFor="existing-credential">Credential (required)</Label>
              {reusable.isLoading ? (
                <Text muted small>
                  Loading credentials…
                </Text>
              ) : reusable.isError ? (
                <FieldError>
                  Credentials could not be loaded. Refresh the page to try
                  again.
                </FieldError>
              ) : (
                <Select
                  required
                  value={existingId}
                  onValueChange={(id) => {
                    setPicked("existing");
                    setExistingId(id);
                  }}
                >
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
                <Label htmlFor="credential-name">Name (required)</Label>
                <Input
                  id="credential-name"
                  required
                  placeholder={
                    choice === "service_account"
                      ? "e.g. Marketing service account"
                      : "e.g. Engineering automation token"
                  }
                  value={name}
                  onChange={setName}
                />
              </div>

              {choice === "remote_session" ? (
                <PersonalSignInFields draft={personal} />
              ) : owner ? (
                <div className="space-y-2">
                  <Label htmlFor="credential-registration">
                    Sign-in (required)
                  </Label>
                  {signIns.isLoading ? (
                    <Text muted small>
                      Loading sign-ins…
                    </Text>
                  ) : signIns.isError ? (
                    <FieldError>
                      Sign-ins could not be loaded. Refresh the page to try
                      again.
                    </FieldError>
                  ) : signIns.choices.length === 0 ? (
                    <Alert variant="info" inline>
                      No sign-in that gets its own token is registered
                      {signIns.upstreamIssuers
                        ? ` with ${signIns.upstreamIssuers}`
                        : ""}
                      {" yet. "}
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
                    <Combobox
                      id="credential-registration"
                      items={signIns.choices.map((choice) => ({
                        value: choice.registration.id,
                        label: choice.label,
                        description: choice.description,
                        keywords: [choice.description],
                        icon: (
                          <IssuerLogo
                            logoAssetId={choice.issuer?.logoAssetId}
                            name={choice.label}
                            size="sm"
                          />
                        ),
                      }))}
                      selected={registrationId}
                      onSelectionChange={(item) =>
                        setRegistrationId(item.value)
                      }
                      searchPlaceholder="Search sign-ins…"
                      emptyMessage="No sign-ins match."
                      className="w-full justify-between"
                      contentClassName="w-[min(500px,calc(100vw-2rem))]"
                    >
                      {signIn ? (
                        <span className="flex min-w-0 items-center gap-2">
                          <IssuerLogo
                            logoAssetId={signIn.issuer?.logoAssetId}
                            name={signIn.label}
                            size="sm"
                          />
                          <span className="truncate">{signIn.label}</span>
                          <span className="text-muted-foreground truncate font-normal">
                            {signIn.description}
                          </span>
                        </span>
                      ) : (
                        "Choose a sign-in"
                      )}
                    </Combobox>
                  )}
                  {!signIns.isLoading &&
                  !signIns.isError &&
                  signIns.upstreamIssuers &&
                  (signIns.hiddenCount > 0 || signIns.showAll) ? (
                    <Text muted small className="block">
                      {signIns.showAll
                        ? "Showing every sign-in, including ones for other services. "
                        : signIns.choices.length > 0
                          ? `Showing sign-ins for ${signIns.upstreamIssuers}, as named by the upstream. `
                          : ""}
                      <button
                        type="button"
                        className="font-medium underline underline-offset-2"
                        onClick={toggleShowAll}
                      >
                        {signIns.showAll
                          ? `Show only ${signIns.upstreamIssuers}`
                          : "Show all"}
                      </button>
                    </Text>
                  ) : null}
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
          {problem ? (
            <Text muted small className="mr-auto self-center">
              {problem}
            </Text>
          ) : null}
          <Button variant="secondary" onClick={close} disabled={pending}>
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
