import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { CodeSnippet } from "@/components/ui/CodeSnippet";
import { CopyButton } from "@/components/ui/CopyButton";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Text } from "@/components/ui/Text";
import { useSdkClient } from "@/contexts/Sdk";
import {
  AvailabilityBadge,
  RadioRows,
  StepMarker,
} from "@/lib/remote-identity";
import { cn } from "@/lib/utils";
import type { JSONWebKeySet } from "@gram/client/models/components/jsonwebkeyset.js";
import type { VerifyKmsKeyResult } from "@gram/client/models/components/verifykmskeyresult.js";
import { useGcpSetupInfo } from "@gram/client/react-query/gcpSetupInfo";
import { useGetGcpIamCredential } from "@gram/client/react-query/getGcpIamCredential";
import { invalidateAllListExternalCredentials } from "@gram/client/react-query/listExternalCredentials";
import { invalidateAllListExternalKeys } from "@gram/client/react-query/listExternalKeys";
import {
  invalidateAllListGcpIamCredentials,
  useListGcpIamCredentials,
} from "@gram/client/react-query/listGcpIamCredentials";
import {
  invalidateAllListGcpKmsKeys,
  useListGcpKmsKeys,
} from "@gram/client/react-query/listGcpKmsKeys";
import { invalidateAllListJsonWebKeySets } from "@gram/client/react-query/listJsonWebKeySets";
import { useQueryClient } from "@tanstack/react-query";
import { Cloud } from "lucide-react";
import { useState } from "react";
import {
  hasBlankResourceField,
  impersonationInstructions,
  keyRingInstructions,
  isKeyVersionResourceName,
  keyVersionResourceName,
  serviceAccountEmail,
  type SetupInstructions,
  type SetupMethod,
  type SetupValues,
  type SigningAlgorithm,
} from "./gcpSetupSnippets";

const SECURITY = { sessionHeaderGramSession: "" };
const NEW = "__new__";
const METHOD_KEY = "speakeasy.keySetSetup.method";

function readMethod(): SetupMethod {
  try {
    const saved = localStorage.getItem(METHOD_KEY);
    if (saved === "gcloud" || saved === "terraform" || saved === "console") {
      return saved;
    }
  } catch {
    // Storage can be unavailable; gcloud is a fine default.
  }
  return "gcloud";
}

function message(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}

// Why a service account didn't verify, and which grant to check.
function impersonationFailure(
  detail: string | undefined,
  grant: string,
): string {
  return `${detail ? `${detail} ` : ""}Check ${grant}. New grants can take a minute or two to apply.`;
}

// What a failed key probe means, and which earlier step fixes it.
function keyProbeMessage(result: VerifyKmsKeyResult): string {
  switch (result.probeOutcome) {
    case "permission_denied":
      return "The service account can't sign with this key. Check the key ring grant above; new grants can take a minute or two to apply.";
    case "key_not_found":
      return "No key version exists at that name. Check the project, location, key ring and key.";
    case "invalid_resource_name":
      return "That isn't a key version's resource name. It should end in /cryptoKeyVersions/ and a number.";
    case "key_unusable":
      return "That key version is disabled or destroyed. Enable it, or use another version.";
    case "unsupported_algorithm":
      return "The key isn't an asymmetric signing key Speakeasy can use, or its algorithm doesn't match the one chosen here.";
    case "algorithm_mismatch":
      return "The key's algorithm doesn't match the one chosen here. Pick the other algorithm and try again.";
    case "credential_unusable":
    case "credential_deleted":
      return "Speakeasy couldn't impersonate the service account. Check the impersonation grant above.";
    case "unavailable":
      return "Cloud KMS didn't answer. Try again in a moment.";
    case "signature_invalid":
    case "unexpected":
    case "verified":
      return result.detail ?? "Speakeasy couldn't sign with this key.";
  }
}

/**
 * Sets up a signing key set from scratch, in the order the cloud side needs
 * it: pick the provider, create a service account Speakeasy may impersonate,
 * connect it (verified), let it sign with a KMS key ring, then register a key
 * version (verified with a real signature) and publish it as a set. Each grant
 * step offers gcloud, Terraform and console instructions, and the choice
 * sticks across steps. A connected service account or registered key skips
 * the steps it would have set up.
 */
export function KeySetSetupSteps({
  defaultName,
  onCreated,
  isCurrentOrganization,
  footer,
}: {
  defaultName: string;
  onCreated: (set: JSONWebKeySet) => void;
  isCurrentOrganization: () => boolean;
  /** Renders the panel's actions with this step's primary action. */
  footer: (action: {
    label: string;
    disabled: boolean;
    pending: boolean;
    run: () => void;
  }) => React.ReactNode;
}): JSX.Element {
  const client = useSdkClient();
  const queryClient = useQueryClient();
  const setupInfo = useGcpSetupInfo(undefined, SECURITY, {
    throwOnError: false,
  });
  const credentialsQuery = useListGcpIamCredentials(undefined, SECURITY, {
    throwOnError: false,
  });
  const keysQuery = useListGcpKmsKeys(undefined, SECURITY, {
    throwOnError: false,
  });
  const credentials = credentialsQuery.data?.credentials ?? [];

  const [method, setMethodState] = useState<SetupMethod>(readMethod);
  const setMethod = (next: SetupMethod) => {
    setMethodState(next);
    try {
      localStorage.setItem(METHOD_KEY, next);
    } catch {
      // Remembering the choice is a convenience only.
    }
  };

  const [credentialChoice, setCredentialChoice] = useState<string | null>(null);
  const usingExisting = !!credentialChoice && credentialChoice !== NEW;
  // A reused service account is granted by its own email, which may not match
  // the default name in the typed project. Until it loads, the derived one
  // stands in.
  const chosenCredential = useGetGcpIamCredential(
    { id: credentialChoice ?? "" },
    SECURITY,
    { enabled: usingExisting, throwOnError: false },
  );

  // A corrected email for a new service account, typed in step 3.
  const [emailEdit, setEmailEdit] = useState<string | null>(null);

  const [values, setValues] = useState<Omit<SetupValues, "speakeasyPrincipal">>(
    {
      projectId: "",
      accountId: "speakeasy-signer",
      location: "global",
      keyRing: "speakeasy",
      keyName: "speakeasy-signing",
    },
  );
  const setupValues: SetupValues = {
    ...values,
    speakeasyPrincipal: setupInfo.data?.serviceAccountEmail ?? "",
    serviceAccountEmail: usingExisting
      ? chosenCredential.data?.impersonateServiceAccount
      : (emailEdit ?? undefined),
  };
  const set = (field: keyof typeof values) => (value: string) =>
    setValues((current) => ({ ...current, [field]: value }));

  // Step 3: the connected service account.
  // The typed value as is, so clearing the field leaves it empty.
  const email = emailEdit ?? serviceAccountEmail(setupValues);
  const [connectedId, setConnectedId] = useState<string | null>(null);
  const [connecting, setConnecting] = useState(false);
  const [connectError, setConnectError] = useState<string | null>(null);
  const credentialId =
    credentialChoice && credentialChoice !== NEW
      ? credentialChoice
      : connectedId;
  const choosingCredential =
    credentials.length > 0 && credentialChoice === null && !connectedId;

  // Step 5: the key and the set it backs.
  const keys = (keysQuery.data?.keys ?? []).filter(
    (key) => key.externalCredentialId === credentialId,
  );
  const [keyChoice, setKeyChoice] = useState<string>(NEW);
  const [resourceEdit, setResourceEdit] = useState<string | null>(null);
  const resourceName = resourceEdit ?? keyVersionResourceName(setupValues);
  const [algorithm, setAlgorithm] = useState<SigningAlgorithm>("ES256");
  const [setName, setSetName] = useState(defaultName);
  // A key registered by an earlier attempt, reused while its name is unchanged.
  const [registered, setRegistered] = useState<{
    id: string;
    resourceName: string;
    algorithm: string;
  } | null>(null);
  const [finishing, setFinishing] = useState(false);
  const [finishError, setFinishError] = useState<string | null>(null);
  // Keys belong to a credential, so switching it drops a choice or
  // registration made under the last one.
  const [keysFor, setKeysFor] = useState(credentialId);
  if (keysFor !== credentialId) {
    setKeysFor(credentialId);
    setKeyChoice(NEW);
    setRegistered(null);
  }

  // A credential created but not yet verified; retries verify it, not a copy.
  const [pendingCredentialId, setPendingCredentialId] = useState<string | null>(
    null,
  );
  const connect = async () => {
    setConnecting(true);
    setConnectError(null);
    let id: string;
    try {
      const created = await client.externalCredentials.createGcpIam(
        {
          createGcpIamCredentialForm: {
            name: email.split("@")[0] || email,
            impersonateServiceAccount: email.trim(),
          },
        },
        SECURITY,
      );
      id = created.id;
      await Promise.all([
        invalidateAllListExternalCredentials(queryClient),
        invalidateAllListGcpIamCredentials(queryClient),
      ]);
    } catch (error) {
      setConnectError(message(error, "Couldn't connect the service account."));
      return;
    } finally {
      setConnecting(false);
    }
    if (!isCurrentOrganization()) return;
    // Kept until it verifies, so "Verify again" re-probes this credential
    // rather than creating another, even when the probe itself fails.
    setPendingCredentialId(id);
    await verifyCredential(id, "the impersonation grant above");
  };
  // Verifies Speakeasy can impersonate the credential's service account, and
  // says which grant to check when it can't.
  const verifyCredential = async (id: string, grant: string) => {
    setConnecting(true);
    setConnectError(null);
    try {
      const verified = await client.externalCredentials.verifyGcpIam(
        { id },
        SECURITY,
      );
      if (!isCurrentOrganization()) return;
      if (verified.verified) {
        setConnectedId(id);
        setPendingCredentialId(null);
      } else {
        setConnectError(impersonationFailure(verified.detail, grant));
      }
    } catch (error) {
      setConnectError(message(error, "Couldn't verify the service account."));
    } finally {
      setConnecting(false);
    }
  };
  const verifyAgain = () => {
    if (pendingCredentialId) {
      void verifyCredential(
        pendingCredentialId,
        "the impersonation grant above",
      );
    }
  };
  // A connected service account is verified again when picked: its grant may
  // have been revoked since it was connected.
  const verifyExisting = (id: string) =>
    void verifyCredential(id, "the service account's impersonation grant");
  const chooseCredential = (choice: string | null) => {
    setCredentialChoice(choice);
    setConnectedId(null);
    setConnectError(null);
    if (choice && choice !== NEW) verifyExisting(choice);
  };

  const finish = async () => {
    if (!credentialId) return;
    setFinishing(true);
    setFinishError(null);
    try {
      let keyId = keyChoice !== NEW ? keyChoice : null;
      if (!keyId) {
        const trimmed = resourceName.trim();
        if (
          registered &&
          registered.resourceName === trimmed &&
          registered.algorithm === algorithm
        ) {
          keyId = registered.id;
        } else {
          const key = await client.externalKeys.createGcpKms(
            {
              createGcpKmsKeyForm: {
                name:
                  trimmed.split("/cryptoKeys/")[1]?.split("/")[0] ?? trimmed,
                externalCredentialId: credentialId,
                resourceName: trimmed,
                algorithm,
              },
            },
            SECURITY,
          );
          keyId = key.id;
          setRegistered({ id: key.id, resourceName: trimmed, algorithm });
          await Promise.all([
            invalidateAllListExternalKeys(queryClient),
            invalidateAllListGcpKmsKeys(queryClient),
          ]);
        }
      }
      // An already registered key is probed too: its grant or version may
      // have changed since it was added.
      const probe = await client.externalKeys.verifyGcpKms(
        { id: keyId },
        SECURITY,
      );
      if (!probe.verified) {
        setFinishError(keyProbeMessage(probe));
        return;
      }
      const created = await client.jsonWebKeySets.create(
        {
          createJSONWebKeySetForm: {
            name: setName.trim(),
            externalKeyId: keyId,
          },
        },
        SECURITY,
      );
      await invalidateAllListJsonWebKeySets(queryClient);
      if (!isCurrentOrganization()) return;
      onCreated(created);
    } catch (error) {
      setFinishError(message(error, "Couldn't create the key set."));
    } finally {
      setFinishing(false);
    }
  };

  const connected = !!credentialId && connectedId === credentialId;
  // A blank field leaves its placeholder in the generated name, which Cloud
  // KMS would only reject after the key is registered. A hand-edited name is
  // checked for its shape instead, since a real segment may share a
  // placeholder's spelling.
  const blankKeyField =
    keyChoice === NEW &&
    resourceEdit === null &&
    hasBlankResourceField(setupValues);
  const malformedKeyName =
    keyChoice === NEW &&
    resourceEdit !== null &&
    !isKeyVersionResourceName(resourceEdit);
  const keyReady =
    keyChoice !== NEW ||
    (resourceEdit === null
      ? !blankKeyField
      : isKeyVersionResourceName(resourceEdit));
  const canFinish =
    connected && keyReady && setName.trim() !== "" && !finishing;

  const principal = setupInfo.data?.serviceAccountEmail;
  // Numbered by what is shown: choosing or reusing an account is one step,
  // setting one up is two.
  const accountSteps = choosingCredential || usingExisting ? 1 : 2;
  const keyRingStep = 2 + accountSteps;
  const keyStep = keyRingStep + 1;
  const credentialName =
    credentials.find((credential) => credential.id === credentialChoice)
      ?.name ?? "a connected service account";

  return (
    <>
      <div className="flex flex-1 flex-col gap-4 overflow-y-auto px-6 py-6">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <Text small muted>
            Speakeasy signs with a key in your cloud KMS. The private half never
            leaves it.
          </Text>
          <SegmentedControl
            value={method}
            onChange={setMethod}
            options={[
              { value: "gcloud" as const, label: "gcloud" },
              { value: "terraform" as const, label: "Terraform" },
              { value: "console" as const, label: "Console" },
            ]}
          />
        </div>

        <ol aria-label="Key set setup" className="bg-card border">
          <Step index={1} done title="Pick a cloud provider">
            <RadioCardGroup
              aria-label="Cloud provider"
              size="sm"
              showIndicator={false}
              orientation="horizontal"
              value="gcp"
              className="grid-cols-1 sm:grid-cols-2"
            >
              <RadioCard
                value="gcp"
                leading={<Cloud className="size-4" />}
                title="Google Cloud"
              >
                Cloud KMS asymmetric signing keys.
              </RadioCard>
              <RadioCard
                value="aws"
                disabled
                leading={<Cloud className="size-4" />}
                title={
                  <span className="flex flex-wrap items-center gap-2">
                    AWS
                    <AvailabilityBadge />
                  </span>
                }
              >
                AWS KMS keys.
              </RadioCard>
            </RadioCardGroup>
          </Step>

          {choosingCredential ? (
            <Step
              index={2}
              done={false}
              title="Use a connected service account, or set up a new one"
            >
              <RadioRows
                label="Service account"
                value={null}
                onValueChange={chooseCredential}
                rows={[
                  ...credentials.map((credential) => ({
                    value: credential.id,
                    title: credential.name,
                    description: "Already connected",
                  })),
                  {
                    value: NEW,
                    title: "Set up a new service account",
                    description: "Create one and let Speakeasy impersonate it.",
                  },
                ]}
              />
            </Step>
          ) : null}

          {usingExisting ? (
            <Step
              index={2}
              done={connected}
              title="Use a connected service account"
            >
              <Text muted small>
                {connecting ? (
                  <>Verifying {credentialName}…</>
                ) : (
                  <>
                    Using {credentialName}.{" "}
                    <button
                      type="button"
                      onClick={() => chooseCredential(null)}
                      className="hover:text-foreground underline underline-offset-2"
                    >
                      Change
                    </button>
                  </>
                )}
              </Text>
              {connectError ? (
                <>
                  <Alert variant="error" dismissible={false}>
                    {connectError}
                  </Alert>
                  <div>
                    <Button
                      variant="secondary"
                      size="sm"
                      onClick={() => verifyExisting(credentialChoice!)}
                    >
                      <Button.Text>Verify again</Button.Text>
                    </Button>
                  </div>
                </>
              ) : null}
            </Step>
          ) : null}

          {!usingExisting && !choosingCredential ? (
            <>
              <Step
                index={2}
                done={!!connectedId}
                title="Create a service account for Speakeasy to impersonate"
              >
                <div className="grid gap-3 sm:grid-cols-2">
                  <Field
                    id="setup-project"
                    label="Google Cloud project ID"
                    value={values.projectId}
                    onChange={set("projectId")}
                    placeholder="acme-prod"
                  />
                  <Field
                    id="setup-account"
                    label="Service account name"
                    value={values.accountId}
                    onChange={set("accountId")}
                  />
                </div>
                {principal ? (
                  <div className="flex flex-col gap-1">
                    <Text muted small>
                      Speakeasy&apos;s service account
                    </Text>
                    <div className="bg-muted/50 flex items-center justify-between gap-2 px-3 py-2 font-mono text-xs">
                      <code className="break-all">{principal}</code>
                      <CopyButton size="xs" text={principal} />
                    </div>
                  </div>
                ) : setupInfo.isLoading ? null : (
                  <Text muted small>
                    This environment can&apos;t report Speakeasy&apos;s service
                    account, so the instructions show SPEAKEASY_PRINCIPAL.
                    Contact support for the principal to grant.
                  </Text>
                )}
                <Instructions
                  instructions={impersonationInstructions(method, setupValues)}
                />
              </Step>

              <Step
                index={3}
                done={!!connectedId}
                title="Connect the service account"
              >
                <Field
                  id="setup-email"
                  label="Service account email"
                  value={email}
                  onChange={(next) => {
                    setEmailEdit(next);
                    // A corrected email is a different service account, so
                    // the next attempt connects it rather than re-probing.
                    setPendingCredentialId(null);
                    setConnectError(null);
                  }}
                  mono
                  disabled={!!connectedId || connecting}
                />
                {connectError ? (
                  <Alert variant="error" dismissible={false}>
                    {connectError}
                  </Alert>
                ) : null}
                {connectedId ? (
                  <Text small className="text-success-foreground">
                    Connected. Speakeasy can impersonate this service account.
                  </Text>
                ) : (
                  <div>
                    <Button
                      variant="secondary"
                      size="sm"
                      disabled={
                        connecting ||
                        !email.includes("@") ||
                        email.includes("PROJECT_ID")
                      }
                      onClick={() =>
                        pendingCredentialId ? verifyAgain() : void connect()
                      }
                    >
                      <Button.Text>
                        {connecting
                          ? "Verifying…"
                          : pendingCredentialId
                            ? "Verify again"
                            : "Connect and verify"}
                      </Button.Text>
                    </Button>
                  </div>
                )}
              </Step>
            </>
          ) : null}

          <Step
            index={keyRingStep}
            done={false}
            muted={!connected}
            title="Let the service account sign with your key ring"
          >
            {connected ? (
              <>
                <Text muted small>
                  The key must be an asymmetric signing key. Pick its algorithm
                  before you create it; the instructions follow the choice.
                </Text>
                <div className="flex flex-col gap-1.5">
                  <Label>Algorithm</Label>
                  <SegmentedControl
                    value={algorithm}
                    onChange={setAlgorithm}
                    options={[
                      {
                        value: "ES256" as const,
                        label: "EC P-256 (ES256)",
                      },
                      {
                        value: "RS256" as const,
                        label: "RSA 2048 (RS256)",
                      },
                    ]}
                  />
                </div>
                <div className="grid gap-3 sm:grid-cols-3">
                  <Field
                    id="setup-location"
                    label="Location"
                    value={values.location}
                    onChange={set("location")}
                  />
                  <Field
                    id="setup-key-ring"
                    label="Key ring"
                    value={values.keyRing}
                    onChange={set("keyRing")}
                  />
                  <Field
                    id="setup-key"
                    label="Key"
                    value={values.keyName}
                    onChange={set("keyName")}
                  />
                </div>
                {usingExisting ? (
                  <Field
                    id="setup-project-existing"
                    label="Google Cloud project ID"
                    value={values.projectId}
                    onChange={set("projectId")}
                    placeholder="acme-prod"
                  />
                ) : null}
                <Instructions
                  instructions={keyRingInstructions(
                    method,
                    setupValues,
                    algorithm,
                  )}
                />
              </>
            ) : (
              <Text muted small>
                Connect the service account first.
              </Text>
            )}
          </Step>

          <Step
            index={keyStep}
            done={false}
            muted={!connected}
            title="Add the key and create the key set"
            last
          >
            {connected ? (
              <>
                {keys.length > 0 ? (
                  <RadioRows
                    label="Key"
                    value={keyChoice}
                    onValueChange={setKeyChoice}
                    rows={[
                      ...keys.map((key) => ({
                        value: key.id,
                        title: key.name,
                        description: "Already registered",
                      })),
                      {
                        value: NEW,
                        title: "Add a key version",
                        description: "By its Cloud KMS resource name.",
                      },
                    ]}
                  />
                ) : null}
                {keyChoice === NEW ? (
                  <Field
                    id="setup-resource"
                    label="Key version resource name"
                    value={resourceName}
                    onChange={setResourceEdit}
                    mono
                  />
                ) : null}
                {blankKeyField ? (
                  <Text small warning>
                    Fill in the placeholders in the key version name to
                    continue.
                  </Text>
                ) : malformedKeyName ? (
                  <Text small warning>
                    Enter a key version name like projects/…/cryptoKeyVersions/1
                    to continue.
                  </Text>
                ) : null}
                <Field
                  id="setup-set-name"
                  label="Key set name"
                  value={setName}
                  onChange={setSetName}
                />
                {finishError ? (
                  <Alert variant="error" dismissible={false}>
                    {finishError}
                  </Alert>
                ) : null}
              </>
            ) : (
              <Text muted small>
                Speakeasy verifies the key with a test signature, then publishes
                it as the key set&apos;s signing key.
              </Text>
            )}
          </Step>
        </ol>
      </div>
      {footer({
        label: finishing
          ? "Creating…"
          : keyChoice === NEW
            ? "Verify key and create key set"
            : "Create key set",
        disabled: !canFinish,
        pending: finishing,
        run: () => void finish(),
      })}
    </>
  );
}

function Step({
  index,
  done,
  title,
  muted = false,
  last = false,
  children,
}: {
  index: number;
  done: boolean;
  title: string;
  muted?: boolean;
  last?: boolean;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <li
      className={cn(
        "flex gap-3 px-4 py-3.5",
        !last && "border-b",
        muted && "opacity-60",
      )}
    >
      <StepMarker index={index} done={done} />
      <div className="flex min-w-0 flex-1 flex-col gap-3">
        <Text small className="font-medium">
          {title}
        </Text>
        {children}
      </div>
    </li>
  );
}

function Field({
  id,
  label,
  value,
  onChange,
  placeholder,
  mono = false,
  disabled = false,
}: {
  id: string;
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  mono?: boolean;
  disabled?: boolean;
}): JSX.Element {
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        value={value}
        onChange={onChange}
        placeholder={placeholder}
        className={cn(mono && "font-mono text-xs")}
        disabled={disabled}
        noAutofill
      />
    </div>
  );
}

function Instructions({
  instructions,
}: {
  instructions: SetupInstructions;
}): JSX.Element {
  if (instructions.kind === "code") {
    return (
      <CodeSnippet
        code={instructions.code}
        language={instructions.language}
        copyable
        wordWrap
      />
    );
  }
  return (
    <ol className="text-muted-foreground flex list-decimal flex-col gap-1.5 pl-5 text-sm">
      {instructions.steps.map((step) => (
        <li key={step}>{step}</li>
      ))}
    </ol>
  );
}
