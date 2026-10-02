import { SetupGuideCallout } from "@/components/setup-guide/SetupGuideCallout";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { Input } from "@/components/ui/Input";
import { Text } from "@/components/ui/Text";
import { useGetMCPSetupDocs } from "@gram/client/react-query/getMCPSetupDocs.js";
import { useEffect, useId, useState } from "react";
import type { UserIdentityDraft } from "../drafts/useIdentityDraft";
import {
  SLACK_DEFAULT_SCOPES,
  SLACK_SCOPE_CHOICES,
  slackAppConfiguration,
} from "./slack";

export function SlackSetup({
  serverUrl,
  draft,
  disabled,
}: {
  serverUrl: string;
  draft: UserIdentityDraft;
  disabled: boolean;
}): JSX.Element | null {
  const descriptionId = useId();
  const { data, isPending } = useGetMCPSetupDocs({ serverUrl }, undefined, {
    throwOnError: false,
  });
  const [newApp, setNewApp] = useState(false);
  const [selectedChoices, setSelectedChoices] = useState<string[]>(
    SLACK_SCOPE_CHOICES.filter((choice) => choice.defaultSelected).map(
      (choice) => choice.label,
    ),
  );
  const setup = draft.slackSetup;
  const manualActive = setup?.manualActive ?? false;
  useEffect(() => {
    if (!manualActive) setNewApp(false);
  }, [manualActive]);
  if (!setup) return null;
  const callback = data?.oauthCallbackUrl;
  const selected = setup.manualActive
    ? SLACK_SCOPE_CHOICES.filter((choice) =>
        choice.scopes.every((scope) => draft.scopes.includes(scope)),
      ).map((choice) => choice.label)
    : selectedChoices;
  const scopes = SLACK_SCOPE_CHOICES.filter((choice) =>
    selected.includes(choice.label),
  ).flatMap((choice) => [...choice.scopes]);
  const configuration = slackAppConfiguration(callback, scopes);
  const callbackValid = !!slackAppConfiguration(callback, SLACK_DEFAULT_SCOPES);
  const toggleChoice = (label: string, checked: boolean): void => {
    const next = checked
      ? [...selected, label]
      : selected.filter((value) => value !== label);
    if (setup.manualActive) {
      draft.setScopes(
        SLACK_SCOPE_CHOICES.filter((choice) =>
          next.includes(choice.label),
        ).flatMap((choice) => [...choice.scopes]),
      );
    } else {
      setSelectedChoices(next);
    }
  };
  const canChooseApp =
    !disabled &&
    !!configuration &&
    (setup.canApplyDefaults ||
      (setup.manualActive &&
        setup.providerCompatible &&
        setup.scopesCompatible));
  const chooseApp = (create: boolean): void => {
    if (!canChooseApp) return;
    if (setup.canApplyDefaults) setup.applyDefaults(scopes);
    setNewApp(create);
  };
  return (
    <div className="mb-6 space-y-3 rounded border p-4">
      <Text className="font-medium">Slack read/search setup</Text>
      <Text small muted className="block">
        Choose the information each authorizing user can access. Two public
        channel options are selected by default. No message sending access is
        requested. See the{" "}
        <a
          href="https://docs.slack.dev/ai/slack-mcp-server/"
          target="_blank"
          rel="noopener noreferrer"
          className="underline"
        >
          Slack MCP permission guide
        </a>
        .
      </Text>
      {!draft.connected && draft.choice !== "existing" && (
        <fieldset className="grid gap-2 sm:grid-cols-2" disabled={disabled}>
          <legend className="mb-2 text-sm font-medium">Slack access</legend>
          {SLACK_SCOPE_CHOICES.map((choice, index) => (
            <label
              key={choice.label}
              className="flex cursor-pointer gap-3 rounded border p-3 text-sm"
            >
              <Checkbox
                checked={selected.includes(choice.label)}
                onCheckedChange={(checked) =>
                  toggleChoice(choice.label, checked === true)
                }
                disabled={disabled}
                aria-label={choice.label}
                aria-describedby={`${descriptionId}-${index}`}
              />
              <span>
                <span className="block font-medium">{choice.label}</span>
                <span
                  id={`${descriptionId}-${index}`}
                  className="text-muted-foreground block"
                >
                  {choice.description}
                </span>
              </span>
            </label>
          ))}
        </fieldset>
      )}
      {!scopes.length && !draft.connected && draft.choice !== "existing" && (
        <Text small warning className="block">
          Choose at least one access option.
        </Text>
      )}
      <Text small className="block">
        Use an eligible internal or Marketplace-published app. Enable MCP, add
        the selected user permissions and this redirect URL without replacing
        unrelated app settings. Workspace approval and individual consent remain
        separate.
      </Text>
      <Input
        aria-label="Slack OAuth callback URL"
        value={callback ?? ""}
        readOnly
      />
      {!isPending && !callbackValid && (
        <Text small warning className="block">
          Could not load a valid deployment callback. Guided defaults and app
          creation are unavailable. Refresh the page to retry; the ordinary
          identity form remains usable.
        </Text>
      )}
      {!setup.providerCompatible && (
        <Text small warning className="block">
          The selected provider does not match Slack's reviewed user-token
          endpoints and Post authentication. It will not be changed
          automatically. Choose a compatible provider below, or configure one
          through Create a custom identity provider.
        </Text>
      )}
      {setup.manualActive && !setup.scopesCompatible && (
        <Text small warning className="block">
          Choose a supported set of read/search access options before saving.
        </Text>
      )}
      <SetupGuideCallout serverUrl={serverUrl} />
      {draft.connected && (
        <Text small muted className="block">
          A saved identity is already configured. Use Clear connection below
          only if you intend to replace it.
        </Text>
      )}
      {!draft.connected && draft.choice === "existing" && (
        <Text small muted className="block">
          A stored client keeps the permissions already saved with it. Choose an
          app option to select different access.
        </Text>
      )}
      {!draft.connected &&
        !setup.manualActive &&
        (draft.clientId || draft.clientSecret || draft.scopeText) && (
          <Text small muted className="block">
            Guided defaults will not overwrite your unsaved edits. Keep editing
            below, or clear the client ID, secret, and scope fields before
            applying defaults.
          </Text>
        )}
      <div className="flex flex-wrap gap-2">
        <Button
          variant="secondary"
          className="h-auto min-h-9 max-w-full whitespace-normal"
          disabled={!canChooseApp || (setup.manualActive && !newApp)}
          onClick={() => chooseApp(false)}
        >
          <Button.Text>Configure an existing Slack app</Button.Text>
        </Button>
        <Button
          variant="secondary"
          className="h-auto min-h-9 max-w-full whitespace-normal"
          disabled={!canChooseApp || (setup.manualActive && newApp)}
          onClick={() => chooseApp(true)}
        >
          <Button.Text>Create a new Slack app</Button.Text>
        </Button>
        {draft.existingAvailable && (
          <Button
            variant="secondary"
            className="h-auto min-h-9 max-w-full whitespace-normal"
            disabled={disabled || draft.connected}
            onClick={() => draft.selectChoice("existing")}
          >
            <Button.Text>Reuse a compatible stored client</Button.Text>
          </Button>
        )}
      </div>
      {newApp && setup.manualActive && configuration && !draft.connected && (
        <SlackAppManifest configuration={configuration} disabled={disabled} />
      )}
      {setup.incompatibleClients.map((client) => (
        <Text key={client.id} small muted className="block">
          Client {client.hint}: {client.reason}. Left unchanged.
        </Text>
      ))}
      <Text small muted className="block">
        Local configuration compatibility does not verify the app's settings in
        Slack. Enter its client ID and secret in the existing form, then Save.
        Saving configures identity only; use Availability if disabled, then
        Inspect / Connect for personal consent.
      </Text>
    </div>
  );
}

function SlackAppManifest({
  configuration,
  disabled,
}: {
  configuration: { json: string; creationUrl: string };
  disabled: boolean;
}): JSX.Element {
  const [copyStatus, setCopyStatus] = useState("");
  const copy = async (): Promise<void> => {
    try {
      await navigator.clipboard.writeText(configuration.json);
      setCopyStatus("Manifest JSON copied.");
    } catch {
      setCopyStatus("Could not copy. Select and copy the JSON below instead.");
    }
  };
  return (
    <div className="space-y-3 border-t pt-3">
      <Text small className="block">
        Create a new internal app in Slack: select your workspace, review these
        read/search permissions, and create the app. Then return here and enter
        its client ID and secret below. App creation does not approve workspace
        access or complete personal consent.
      </Text>
      <div className="flex flex-wrap gap-2">
        {!disabled && (
          <Button
            variant="secondary"
            className="h-auto min-h-9 max-w-full whitespace-normal"
            asChild
          >
            <a
              href={configuration.creationUrl}
              target="_blank"
              rel="noopener noreferrer"
            >
              Open Slack app creation
            </a>
          </Button>
        )}
        <Button
          variant="secondary"
          className="h-auto min-h-9 max-w-full whitespace-normal"
          disabled={disabled}
          onClick={() => void copy()}
        >
          <Button.Text>Copy manifest JSON</Button.Text>
        </Button>
      </div>
      <Text small className="block" role="status">
        {copyStatus}
      </Text>
      <details>
        <summary className="cursor-pointer text-sm">Manifest JSON</summary>
        <pre
          className="mt-2 max-w-full overflow-x-auto text-xs"
          aria-label="Slack app manifest JSON"
        >
          {configuration.json}
        </pre>
      </details>
    </div>
  );
}
