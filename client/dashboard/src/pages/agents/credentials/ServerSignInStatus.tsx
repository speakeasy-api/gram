import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { CreateMcpAuthenticationProvider } from "./CreateMcpAuthenticationProvider";
import type { JSX } from "react";
import type { SetupServer } from "./setupInstructions";
import { ServerSignInSettingsLink, SignInSelect } from "./SetupFields";
import { SwitchServerSignIn } from "./SwitchServerSignIn";
import { useSwitchAvailability } from "./useSwitchAvailability";
import type { ServerSignIn, SetupValues } from "./workloadTokenEndpoints";

/**
 * The sign-in an agent running on a platform gets its token from, chosen while
 * its servers are: the token works only at the servers that use it, so the
 * list below says which ones those are.
 */
export function SignInPicker({
  setupValues,
}: {
  setupValues: SetupValues;
}): JSX.Element {
  if (setupValues.loadState === "loading") {
    return (
      <Text muted small>
        Loading MCP authentication providers…
      </Text>
    );
  }
  if (setupValues.loadState === "error") {
    return (
      <Text muted small>
        Speakeasy couldn&apos;t load its MCP authentication providers. Refresh
        to try again.
      </Text>
    );
  }
  if (setupValues.endpointGroups.length === 0) {
    return (
      <CreateMcpAuthenticationProvider
        onCreated={setupValues.refreshAndSelect}
      />
    );
  }
  return (
    <Stack gap={1}>
      <SignInSelect setupValues={setupValues} id="mcp-selection-sign-in" />
      <Text muted small>
        The agent gets one token, which works at the servers that use this
        provider.
      </Text>
    </Stack>
  );
}

/** One line under a server's name saying how it signs in. */
export function ServerSignInNote({
  name,
  signIn,
}: {
  name: string;
  signIn: ServerSignIn;
}): JSX.Element | null {
  switch (signIn.status) {
    case "no_sign_in":
      return null;
    case "covered":
      return <>Uses {signIn.label}</>;
    case "another_issuer":
      return (
        <>
          Uses a different MCP authentication provider ({signIn.label}), so this
          token won&apos;t work at {name}
        </>
      );
    case "not_shared":
      return (
        <>
          Uses its own MCP authentication provider, which can&apos;t give tokens
          to agents
        </>
      );
  }
}

/**
 * The fix for a chosen server the token won't work at: assigning the shared
 * sign-in to it, or its settings when that is not safe or not allowed here.
 */
export function ServerSignInAction({
  server,
  signIn,
}: {
  server: SetupServer;
  signIn: ServerSignIn;
}): JSX.Element | null {
  if (signIn.status !== "not_shared") return null;
  if (!signIn.switchTo) return <ServerSignInSettingsLink server={server} />;
  return <AssignOrSettings server={server} target={signIn.switchTo} />;
}

function AssignOrSettings({
  server,
  target,
}: {
  server: SetupServer;
  target: { id: string; label: string };
}): JSX.Element | null {
  const availability = useSwitchAvailability(server);
  switch (availability) {
    case "ready":
      return <SwitchServerSignIn server={server} target={target} />;
    case "checking":
      return (
        <Button variant="secondary" size="sm" disabled>
          <Button.Text>
            Assign {target.label} to {server.name}
          </Button.Text>
        </Button>
      );
    case "people_connect":
    case "check_failed":
    case "no_permission":
      return <ServerSignInSettingsLink server={server} />;
  }
}
