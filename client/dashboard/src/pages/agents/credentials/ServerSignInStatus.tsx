import { Button } from "@/components/ui/Button";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import type { JSX } from "react";
import type { SetupServer } from "./setupInstructions";
import { ServerSignInSettingsLink, SignInSelect } from "./SetupFields";
import { SwitchServerSignIn } from "./SwitchServerSignIn";
import { useSwitchAvailability } from "./useSwitchAvailability";
import type { ServerSignIn, SetupValues } from "./workloadTokenEndpoints";

export type SignInMode = "api-key" | "workload";

/**
 * How the agent will sign in, asked before its servers are chosen: an agent
 * running on a platform gets one token, which only the servers sharing its
 * sign-in accept, so the list below shows which ones those are.
 */
export function SignInChoice({
  mode,
  onModeChange,
  canUsePlatform,
  disabled,
  setupValues,
}: {
  mode: SignInMode;
  onModeChange: (mode: SignInMode) => void;
  /** Whether the reader may set up an agent that runs on a platform. */
  canUsePlatform: boolean;
  /** Settled for an agent that already exists. */
  disabled: boolean;
  setupValues: SetupValues;
}): JSX.Element {
  return (
    <Stack gap={3}>
      <Stack gap={2}>
        <Text small>How will this agent sign in?</Text>
        <SegmentedControl<SignInMode>
          ariaLabel="How will this agent sign in?"
          value={mode}
          onChange={onModeChange}
          disabled={disabled}
          options={[
            { value: "api-key", label: "API key" },
            {
              value: "workload",
              label: "Agent running on a platform",
              disabled: !canUsePlatform,
            },
          ]}
        />
        {!canUsePlatform && !disabled && (
          <Text muted small>
            Setting up an agent that runs on a platform needs permission to
            manage workload identities.
          </Text>
        )}
      </Stack>
      {mode === "workload" && (
        <Stack gap={1}>
          <SignInSelect setupValues={setupValues} id="mcp-selection-sign-in" />
          <Text muted small>
            The agent gets one token, which works at the servers that use this
            sign-in.
          </Text>
        </Stack>
      )}
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
          Uses a different sign-in ({signIn.label}), so this token won&apos;t
          work at {name}
        </>
      );
    case "not_shared":
      return <>Uses its own sign-in, which can&apos;t give tokens to agents</>;
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
