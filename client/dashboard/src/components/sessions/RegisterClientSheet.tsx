import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { CreateUserSessionClientResult } from "@gram/client/models/components/createusersessionclientresult.js";
import { CreateUserSessionClientRequestBodyTokenEndpointAuthMethod as AuthMethod } from "@gram/client/models/components/createusersessionclientrequestbody.js";
import { useCreateUserSessionClientMutation } from "@gram/client/react-query/createUserSessionClient.js";
import { invalidateAllUserSessionClients } from "@gram/client/react-query/userSessionClients.js";

import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/RadioGroup";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Text } from "@/components/ui/Text";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { RedirectUriInputs } from "./RedirectUriInputs";
import { filledRedirectUris } from "./redirect-uris";

type AuthMethodValue = (typeof AuthMethod)[keyof typeof AuthMethod];

const AUTH_METHOD_OPTIONS: { value: AuthMethodValue; description: string }[] = [
  {
    value: AuthMethod.ClientSecretBasic,
    description:
      "HTTP Basic: client_id:client_secret in the Authorization header",
  },
  {
    value: AuthMethod.ClientSecretPost,
    description: "client_id and client_secret in the token request form body",
  },
];

/**
 * Registers an agent that cannot register itself. Speakeasy mints the client
 * ID and secret, so the sheet asks only for what the agent's owner knows, then
 * shows the credentials once. The secret is held in component state only and
 * is gone when the sheet closes.
 */
export function RegisterClientSheet({
  issuerId,
  open,
  onOpenChange,
}: {
  issuerId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const gramProject = useProjectSlugForRequests();

  const [name, setName] = useState("");
  const [redirectUris, setRedirectUris] = useState<string[]>([""]);
  const [authMethod, setAuthMethod] = useState<AuthMethodValue>(
    AuthMethod.ClientSecretBasic,
  );
  const [registered, setRegistered] =
    useState<CreateUserSessionClientResult | null>(null);

  const create = useCreateUserSessionClientMutation({
    onSuccess: async (result) => {
      setRegistered(result);
      await invalidateAllUserSessionClients(queryClient, {
        refetchType: "all",
      });
    },
  });

  const reset = () => {
    setName("");
    setRedirectUris([""]);
    setAuthMethod(AuthMethod.ClientSecretBasic);
    setRegistered(null);
    create.reset();
  };

  const handleOpenChange = (next: boolean) => {
    if (!next) reset();
    onOpenChange(next);
  };

  const uris = filledRedirectUris(redirectUris);
  const canSubmit = name.trim() !== "" && uris.length > 0 && !create.isPending;

  const submit = () => {
    create.mutate({
      request: {
        gramProject,
        createUserSessionClientRequestBody: {
          userSessionIssuerId: issuerId,
          clientName: name.trim(),
          redirectUris: uris,
          tokenEndpointAuthMethod: authMethod,
        },
      },
    });
  };

  return (
    <Sheet open={open} onOpenChange={handleOpenChange}>
      <SheetContent className="w-full gap-0 overflow-y-auto sm:max-w-lg">
        {registered ? (
          <RegisteredCredentials
            result={registered}
            onDone={() => handleOpenChange(false)}
          />
        ) : (
          <form
            className="flex h-full flex-col"
            onSubmit={(event) => {
              event.preventDefault();
              if (canSubmit) submit();
            }}
          >
            <SheetHeader>
              <SheetTitle>Register an agent</SheetTitle>
              <SheetDescription>
                For an agent that can’t register itself. Speakeasy creates the
                client ID and secret for you to hand to whoever runs the agent.
              </SheetDescription>
            </SheetHeader>

            <div className="flex flex-col gap-6 px-4 pb-6">
              <div className="flex flex-col gap-2">
                <Label htmlFor="register-client-name">Name</Label>
                <Input
                  id="register-client-name"
                  value={name}
                  onChange={setName}
                  placeholder="Support triage agent"
                  disabled={create.isPending}
                />
                <Text small muted>
                  Shown in this list and on the consent screen people see when
                  it signs in.
                </Text>
              </div>

              <fieldset className="flex flex-col gap-2">
                <legend className="mb-2 text-sm">Redirect URIs</legend>
                <RedirectUriInputs
                  value={redirectUris}
                  onChange={setRedirectUris}
                  disabled={create.isPending}
                />
                <Text small muted>
                  Exact match on every /authorize. At least one is required.
                </Text>
              </fieldset>

              <fieldset className="flex flex-col gap-2">
                <legend className="mb-2 text-sm">
                  Token endpoint auth method
                </legend>
                <RadioGroup
                  value={authMethod}
                  onValueChange={(value) =>
                    setAuthMethod(value as AuthMethodValue)
                  }
                  disabled={create.isPending}
                >
                  {AUTH_METHOD_OPTIONS.map((option) => {
                    const id = `register-client-auth-${option.value}`;
                    return (
                      <div
                        key={option.value}
                        className="flex items-start gap-3"
                      >
                        <RadioGroupItem
                          value={option.value}
                          id={id}
                          className="mt-1"
                        />
                        <Label
                          htmlFor={id}
                          className="flex flex-col items-start gap-0.5"
                        >
                          <span className="font-mono text-xs">
                            {option.value}
                          </span>
                          <Text small muted>
                            {option.description}
                          </Text>
                        </Label>
                      </div>
                    );
                  })}
                </RadioGroup>
              </fieldset>

              {create.error && (
                <Alert variant="error" title="Couldn’t register the agent">
                  {create.error.message}
                </Alert>
              )}
            </div>

            <SheetFooter className="mt-auto flex-row justify-end gap-2 border-t">
              <Button
                variant="secondary"
                size="sm"
                type="button"
                onClick={() => handleOpenChange(false)}
                disabled={create.isPending}
              >
                <Button.Text>Cancel</Button.Text>
              </Button>
              <Button size="sm" type="submit" disabled={!canSubmit}>
                <Button.Text>
                  {create.isPending ? "Registering…" : "Register agent"}
                </Button.Text>
              </Button>
            </SheetFooter>
          </form>
        )}
      </SheetContent>
    </Sheet>
  );
}

function RegisteredCredentials({
  result,
  onDone,
}: {
  result: CreateUserSessionClientResult;
  onDone: () => void;
}): JSX.Element {
  const { client, clientSecret } = result;
  const asJson = JSON.stringify(
    { client_id: client.clientId, client_secret: clientSecret },
    null,
    2,
  );

  return (
    <div className="flex h-full flex-col">
      <SheetHeader>
        <SheetTitle className="truncate" title={client.clientName}>
          {client.clientName} is registered
        </SheetTitle>
        <SheetDescription>
          Give these to whoever runs the agent.
        </SheetDescription>
      </SheetHeader>

      <div className="flex flex-col gap-6 px-4 pb-6">
        <Alert variant="warning" title={null}>
          Save the secret now. It will only be shown once.
        </Alert>

        <CredentialField label="Client ID" value={client.clientId} />
        <CredentialField label="Client secret" value={clientSecret} />

        <div className="border-border flex flex-col gap-4 border-t pt-4">
          <div className="flex flex-col gap-1">
            <Text small muted>
              Token endpoint auth method
            </Text>
            <Text small className="font-mono">
              {client.tokenEndpointAuthMethod}
            </Text>
          </div>
          <div className="flex flex-col gap-1">
            <Text small muted>
              Redirect URIs
            </Text>
            {client.redirectUris.map((uri) => (
              <Text key={uri} small className="font-mono break-all">
                {uri}
              </Text>
            ))}
          </div>
        </div>
      </div>

      <SheetFooter className="mt-auto flex-row justify-between gap-2 border-t">
        <Button
          variant="secondary"
          size="sm"
          onClick={() => void navigator.clipboard.writeText(asJson)}
        >
          <Button.Text>Copy both as JSON</Button.Text>
        </Button>
        <Button size="sm" onClick={onDone}>
          <Button.Text>Done</Button.Text>
        </Button>
      </SheetFooter>
    </div>
  );
}

function CredentialField({
  label,
  value,
}: {
  label: string;
  value: string;
}): JSX.Element {
  return (
    <div className="flex flex-col gap-2">
      <Text small>{label}</Text>
      <div className="border-border flex items-center gap-2 border px-3 py-2">
        <Text small className="min-w-0 flex-1 font-mono break-all">
          {value}
        </Text>
        <CopyButton
          text={value}
          size="sm"
          tooltip={`Copy ${label.toLowerCase()}`}
        />
      </div>
    </div>
  );
}
