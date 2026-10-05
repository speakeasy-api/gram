import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { Text } from "@/components/ui/Text";
import { useGetMCPSetupDocs } from "@gram/client/react-query/getMCPSetupDocs.js";
import { useEffect, useId, useState, type ReactNode } from "react";
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
  children,
}: {
  serverUrl: string;
  draft: UserIdentityDraft;
  disabled: boolean;
  children: ReactNode;
}): JSX.Element | null {
  const descriptionId = useId();
  const { data, isPending } = useGetMCPSetupDocs({ serverUrl }, undefined, {
    throwOnError: false,
  });
  const [openedManifest, setOpenedManifest] = useState<string | null>(null);
  const [selectedChoices, setSelectedChoices] = useState<string[]>(
    SLACK_SCOPE_CHOICES.filter((choice) => choice.defaultSelected).map(
      (choice) => choice.label,
    ),
  );
  const setup = draft.guidedSetup;
  useEffect(() => {
    if (
      setup?.canApplyDefaults &&
      !setup.manualActive &&
      draft.status.kind !== "done" &&
      draft.choice === "manual" &&
      draft.scopes.length === 0
    ) {
      setup.applyDefaults(
        SLACK_SCOPE_CHOICES.filter((choice) =>
          selectedChoices.includes(choice.label),
        ).flatMap((choice) => [...choice.scopes]),
      );
    }
  }, [
    setup,
    draft.choice,
    draft.scopes.length,
    draft.status.kind,
    selectedChoices,
  ]);
  if (!setup) return null;
  const callback = data?.oauthCallbackUrl;
  const selected =
    setup.manualActive || draft.scopes.length > 0
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
    const nextScopes = SLACK_SCOPE_CHOICES.filter((choice) =>
      next.includes(choice.label),
    ).flatMap((choice) => [...choice.scopes]);
    if (setup.manualActive) {
      draft.setScopes(nextScopes);
    } else {
      setSelectedChoices(next);
      if (setup.canApplyDefaults) setup.applyDefaults(nextScopes);
    }
  };
  const canChooseApp =
    !disabled &&
    !!configuration &&
    (setup.canApplyDefaults ||
      (setup.manualActive &&
        setup.providerCompatible &&
        setup.scopesCompatible));
  const openApp = (): void => {
    if (!canChooseApp || !configuration) return;
    if (setup.canApplyDefaults) setup.applyDefaults(scopes);
    setOpenedManifest(configuration.json);
  };
  if (draft.connected || draft.status.kind === "done")
    return (
      <div className="space-y-3">
        {children}
        <Text small muted className="block">
          Slack app configured.
        </Text>
      </div>
    );
  const reusing = draft.choice === "existing";
  return (
    <div className="mb-6 space-y-3 rounded border p-4">
      <div className="flex items-center justify-between gap-3">
        <Text className="font-medium">Slack Setup</Text>
        {draft.cleared && (
          <Button
            variant="tertiary"
            size="sm"
            disabled={disabled}
            onClick={draft.cancelClear}
          >
            <Button.Text>Cancel</Button.Text>
          </Button>
        )}
      </div>
      <Text small muted className="block">
        Create a Slack app, then save its credentials here.
      </Text>
      {draft.existingAvailable && (
        <div className="flex flex-wrap gap-2">
          <Button
            variant="secondary"
            disabled={disabled || !reusing}
            onClick={() => draft.selectChoice("manual")}
          >
            <Button.Text>Set up a new Slack app</Button.Text>
          </Button>
          <Button
            variant="secondary"
            disabled={disabled || reusing}
            onClick={() => draft.selectChoice("existing")}
          >
            <Button.Text>Use a saved Slack app</Button.Text>
          </Button>
        </div>
      )}
      {!draft.connected && draft.choice !== "existing" && (
        <fieldset className="grid gap-2 sm:grid-cols-2" disabled={disabled}>
          <legend className="mb-2 text-sm font-medium">1. Choose access</legend>
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
          automatically. Choose a compatible provider in Advanced, or configure
          one through Create a custom identity provider.
        </Text>
      )}
      {!reusing &&
        (setup.manualActive || draft.scopes.length > 0) &&
        !setup.scopesCompatible && (
          <Text small warning className="block">
            Choose a supported set of read/search access options before saving.
          </Text>
        )}
      {!reusing && (
        <div className="space-y-3 border-t pt-3">
          <Text className="block font-medium">2. Create your Slack app</Text>
          <Text small muted className="block">
            The manifest includes MCP enablement, the selected permissions, and
            the callback URL. In Slack, select your workspace and create the
            app.
          </Text>
          {canChooseApp && configuration ? (
            <Button variant="primary" asChild>
              <a
                href={configuration.creationUrl}
                target="_blank"
                rel="noopener noreferrer"
                onClick={openApp}
              >
                Create app in Slack ↗
              </a>
            </Button>
          ) : (
            <Button disabled>Create app in Slack ↗</Button>
          )}
          {openedManifest &&
            configuration &&
            openedManifest !== configuration.json && (
              <Text small warning className="block">
                Access changed after opening Slack. If you already created the
                app, update its permissions in Slack or create it again with
                this manifest.
              </Text>
            )}
          {configuration && (
            <details>
              <summary className="cursor-pointer text-sm">Manual setup</summary>
              <SlackAppManifest
                configuration={configuration}
                disabled={disabled}
              />
            </details>
          )}
        </div>
      )}
      {reusing ? (
        <div className="space-y-2">
          <Text small muted className="block">
            This app keeps its saved permissions. Choose it below, then Save.
          </Text>
          <ul
            aria-label="Saved Slack app access"
            className="list-inside list-disc text-sm"
          >
            {SLACK_SCOPE_CHOICES.filter((choice) =>
              choice.scopes.every((scope) =>
                draft.existingOptions
                  .find((client) => client.id === draft.existingClientId)
                  ?.scopes.includes(scope),
              ),
            ).map((choice) => (
              <li key={choice.label}>{choice.label}</li>
            ))}
          </ul>
        </div>
      ) : (
        <div className="space-y-1 border-t pt-3">
          <Text className="block font-medium">3. Add app credentials</Text>
          <Text small muted className="block">
            In your Slack app, open Basic Information → App Credentials. Copy
            the Client ID and Client Secret into the fields below, then Save
            configuration.
          </Text>
        </div>
      )}
      {children}
      <div className="space-y-1 border-t pt-3">
        <Text className="block font-medium">
          {reusing ? "Save configuration" : "4. Save configuration"}
        </Text>
        <Text small muted className="block">
          Use Save below to store the app configuration.
        </Text>
      </div>
      {setup.incompatibleClients.map((client) => (
        <Text key={client.id} small muted className="block">
          Client {client.hint}: {client.reason}. Left unchanged.
        </Text>
      ))}
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
      <div className="flex flex-wrap gap-2">
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
