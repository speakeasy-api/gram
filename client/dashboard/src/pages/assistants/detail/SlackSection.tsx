import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { StatusDot } from "@/components/ui/StatusDot";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { useOrganization, useProject } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { firstPartyHosts } from "@/lib/utils";
import type { Assistant } from "@gram/client/models/components/assistant.js";
import type { TriggerInstance } from "@gram/client/models/components/triggerinstance.js";
import { useMutation } from "@tanstack/react-query";
import { ExternalLink } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { applySlackSetup, removeSlack, saveEnvEntries } from "./assistantSetup";
import { Row, Section } from "./PanelSection";
import { ASSISTANT_SLACK_SECTION_ID } from "./sectionIds";
import {
  DEFAULT_SLACK_REPLY_MODE,
  SLACK_CAPABILITY_GROUPS,
  SLACK_EVENT_GROUPS,
  slackReplyModeLabel,
  type SlackReplyMode,
} from "./slackCapabilities";
import {
  SLACK_CREDENTIALS,
  type SlackConnection,
  type SlackCredentialKey,
} from "./slackConnection";
import { buildSlackManifest } from "./slackManifest";
import { SlackReplyModeOptions } from "./SlackReplyModeOptions";
import { useAssistantDraft } from "./useAssistantDraft";

/**
 * Slack, start to finish: what the assistant can do there and when it
 * replies, then installing its Slack app and pasting the app's credentials.
 */
export function SlackSection({
  assistant,
  slack,
  triggers,
}: {
  assistant: Assistant;
  slack: SlackConnection;
  triggers: readonly TriggerInstance[];
}): JSX.Element {
  const project = useProject();
  const sdk = useSdkClient();
  const organization = useOrganization();
  const draft = useAssistantDraft();
  const { hasScope } = useRBAC();
  const canWrite = hasScope("assistant:write", assistant.id, project.id);
  const [setupOpen, setSetupOpen] = useState(false);
  const [confirmRemove, setConfirmRemove] = useState(false);
  const [editingTokens, setEditingTokens] = useState(false);

  const remove = useMutation({
    mutationFn: () => removeSlack(sdk, assistant, triggers),
    onSuccess: (updated) => {
      draft.setAssistant(updated);
      draft.invalidateAll();
      setConfirmRemove(false);
      toast.success("Slack removed");
    },
  });

  const save = useMutation({
    mutationFn: (choice: Parameters<typeof applySlackSetup>[4]) =>
      applySlackSetup(sdk, organization.id, assistant, triggers, choice),
    onSuccess: ({ assistant: updated }) => {
      draft.setAssistant(updated);
      draft.invalidateAll();
      setSetupOpen(false);
      toast.success("Slack setup saved");
    },
  });

  const needsInstall = slack.connected && slack.missing.length > 0;
  const capabilityLabels = SLACK_CAPABILITY_GROUPS.filter((g) =>
    slack.capabilities.includes(g.slug),
  ).map((g) => g.label);

  return (
    <Section
      id={ASSISTANT_SLACK_SECTION_ID}
      title="Slack"

      action={
        canWrite && slack.connected ? (
          <div className="flex items-center gap-2">
            <Button variant="tertiary" onClick={() => setConfirmRemove(true)}>
              Remove
            </Button>
            <Button variant="secondary" onClick={() => setSetupOpen(true)}>
              Change
            </Button>
          </div>
        ) : undefined
      }
    >
      {!slack.connected ? (
        <div className="flex flex-wrap items-center justify-between gap-4">
          <Text small muted className="max-w-xl">
            Not connected.
          </Text>
          {canWrite ? (
            <Button onClick={() => setSetupOpen(true)}>Connect Slack</Button>
          ) : null}
        </div>
      ) : (
        <div className="flex flex-col gap-6">
          <div className="max-w-2xl">
            <Row label="Replies">
              <Text small>
                {slack.trigger
                  ? (slackReplyModeLabel(slack.replyMode) ?? "When @-mentioned")
                  : "Never"}
              </Text>
            </Row>
            <Row label="Can">
              <Text small className="text-right">
                {capabilityLabels.length > 0
                  ? capabilityLabels.join(", ")
                  : "Nothing yet"}
              </Text>
            </Row>
            <Row label="Slack app">
              <span className="flex items-center gap-2">
                <StatusDot tone={needsInstall ? "warning" : "success"} />
                <Text small>
                  {needsInstall ? "Not installed yet" : "Installed"}
                </Text>
                {!needsInstall && canWrite && !editingTokens ? (
                  <Button
                    variant="tertiary"
                    size="sm"
                    onClick={() => setEditingTokens(true)}
                  >
                    Replace credentials
                  </Button>
                ) : null}
              </span>
            </Row>
          </div>
          {canWrite && (needsInstall || editingTokens) ? (
            <SlackInstall
              assistant={assistant}
              slack={slack}
              onDone={() => setEditingTokens(false)}
              onCancel={editingTokens ? () => setEditingTokens(false) : null}
            />
          ) : null}
        </div>
      )}

      <SlackSetupDialog
        open={setupOpen}
        onOpenChange={setSetupOpen}
        slack={slack}
        saving={save.isPending}
        error={save.error ? "Couldn't save the Slack setup. Try again." : null}
        onSave={(choice) => save.mutate(choice)}
      />
      <ConfirmDialog
        open={confirmRemove}
        onOpenChange={setConfirmRemove}
        title={`Remove Slack from ${assistant.name}?`}
        description="It loses its Slack tools and stops replying in Slack. Its Slack app stays installed until you remove it in Slack."
        confirmLabel="Remove Slack"
        pendingLabel="Removing…"
        confirmVariant="destructive-primary"
        isPending={remove.isPending}
        error={remove.error ? "Couldn't remove Slack. Try again." : undefined}
        onConfirm={() => remove.mutate()}
      />
    </Section>
  );
}

function SlackSetupDialog({
  open,
  onOpenChange,
  slack,
  saving,
  error,
  onSave,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  slack: SlackConnection;
  saving: boolean;
  error: string | null;
  onSave: (choice: {
    capabilities: string[];
    events: string[];
    replyMode: SlackReplyMode | undefined;
  }) => void;
}): JSX.Element {
  const initial = () => ({
    caps: new Set(slack.connected ? slack.capabilities : ["send", "read"]),
    events: new Set(slack.events),
    listens: !slack.connected || !!slack.trigger,
    replyMode: slack.replyMode ?? DEFAULT_SLACK_REPLY_MODE,
  });
  const [state, setState] = useState(initial);
  const handleOpenChange = (next: boolean) => {
    if (next) setState(initial());
    onOpenChange(next);
  };
  const toggle = (key: "caps" | "events", slug: string) =>
    setState((prev) => {
      const nextSet = new Set(prev[key]);
      if (nextSet.has(slug)) nextSet.delete(slug);
      else nextSet.add(slug);
      return { ...prev, [key]: nextSet };
    });

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <Dialog.Content className="flex max-h-[85vh] w-[min(92vw,640px)] flex-col gap-4">
        <Dialog.Title>
          {slack.connected ? "Change Slack setup" : "Connect Slack"}
        </Dialog.Title>

        <div className="-mx-6 min-h-0 flex-1 space-y-6 overflow-y-auto px-6">
          <fieldset className="space-y-2">
            <legend className="text-eyebrow mb-2">Capabilities</legend>
            <OptionList
              idPrefix="slack-cap"
              options={SLACK_CAPABILITY_GROUPS}
              selected={state.caps}
              onToggle={(slug) => toggle("caps", slug)}
            />
          </fieldset>

          <fieldset className="space-y-3">
            <div className="flex items-center justify-between gap-4">
              <span id="slack-listens" className="text-sm font-medium">
                Reply to messages in Slack
              </span>
              <Switch
                aria-labelledby="slack-listens"
                checked={state.listens}
                onCheckedChange={(listens) =>
                  setState((prev) => ({ ...prev, listens }))
                }
              />
            </div>
            {state.listens ? (
              <>
                <SlackReplyModeOptions
                  idPrefix="slack-reply"
                  value={state.replyMode}
                  onChange={(replyMode) =>
                    setState((prev) => ({ ...prev, replyMode }))
                  }
                />
                <div className="space-y-2 pt-2">
                  <span className="text-eyebrow block">Extra events</span>
                  <OptionList
                    idPrefix="slack-event"
                    options={SLACK_EVENT_GROUPS}
                    selected={state.events}
                    onToggle={(slug) => toggle("events", slug)}
                  />
                </div>
              </>
            ) : (
              <Text small muted>
                It can still post when a schedule or webhook runs it.
              </Text>
            )}
          </fieldset>
          {error ? (
            <Text small className="text-destructive">
              {error}
            </Text>
          ) : null}
        </div>

        <Dialog.Footer>
          <Button variant="tertiary" onClick={() => handleOpenChange(false)}>
            Cancel
          </Button>
          <Button
            disabled={saving || (state.caps.size === 0 && !state.listens)}
            onClick={() =>
              onSave({
                capabilities: Array.from(state.caps),
                events: state.listens ? Array.from(state.events) : [],
                replyMode: state.listens ? state.replyMode : undefined,
              })
            }
          >
            {saving ? "Saving…" : "Save"}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

function OptionList({
  idPrefix,
  options,
  selected,
  onToggle,
}: {
  idPrefix: string;
  options: readonly { slug: string; label: string; description: string }[];
  selected: Set<string>;
  onToggle: (slug: string) => void;
}): JSX.Element {
  return (
    <div className="divide-border divide-y border">
      {options.map((option) => {
        const id = `${idPrefix}-${option.slug}`;
        return (
          <label
            key={option.slug}
            htmlFor={id}
            className="hover:bg-muted/50 flex cursor-pointer items-start gap-3 px-3 py-2.5"
          >
            <Checkbox
              id={id}
              checked={selected.has(option.slug)}
              onCheckedChange={() => onToggle(option.slug)}
              className="mt-0.5"
            />
            <span className="flex flex-col">
              <Text small className="font-medium">
                {option.label}
              </Text>
              <Text small muted>
                {option.description}
              </Text>
            </span>
          </label>
        );
      })}
    </div>
  );
}

/**
 * Installing the app: open Slack with a ready-made app manifest, install
 * it, then paste the credentials Slack shows into the assistant's
 * environment.
 */
function SlackInstall({
  assistant,
  slack,
  onDone,
  onCancel,
}: {
  assistant: Assistant;
  slack: SlackConnection;
  onDone: () => void;
  onCancel: (() => void) | null;
}): JSX.Element {
  const sdk = useSdkClient();
  const draft = useAssistantDraft();
  const keys: SlackCredentialKey[] = [...slack.required, "SLACK_USER_TOKEN"];
  const [values, setValues] = useState<Record<string, string>>({});
  const webhookUrl = slack.trigger?.webhookUrl;
  const { deepLink } = useMemo(
    () =>
      buildSlackManifest({
        appName: assistant.name,
        webhookUrl,
        unfurlDomains: firstPartyHosts(window.location.origin),
      }),
    [assistant.name, webhookUrl],
  );

  const save = useMutation({
    mutationFn: async () => {
      if (!slack.env) throw new Error("no environment");
      await saveEnvEntries(
        sdk,
        slack.env.slug,
        keys
          .filter((key) => (values[key] ?? "").trim() !== "")
          .map((key) => ({ name: key, value: values[key]!.trim() })),
      );
    },
    onSuccess: () => {
      draft.invalidateAll();
      setValues({});
      toast.success("Slack credentials saved");
      onDone();
    },
  });

  const requiredFilled = slack.missing.every(
    (key) => (values[key] ?? "").trim() !== "",
  );
  const anyFilled = keys.some((key) => (values[key] ?? "").trim() !== "");

  const steps = [
    "Open Slack, pick a workspace, click Create.",
    "Install App → Install to Workspace.",
    webhookUrl ? "Event Subscriptions → Retry → Save Changes." : null,
    "Paste the credentials below.",
  ].filter((s): s is string => s !== null);

  return (
    <div className="border-border flex flex-col gap-5 border-t pt-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <ol className="max-w-xl space-y-2 text-sm">
          {steps.map((step, i) => (
            <li key={step} className="flex gap-3">
              <span className="text-muted-foreground w-4 shrink-0 tabular-nums">
                {i + 1}.
              </span>
              <span>{step}</span>
            </li>
          ))}
        </ol>
        <Button variant="secondary" asChild>
          <a href={deepLink} target="_blank" rel="noopener noreferrer">
            Open Slack
            <ExternalLink className="ml-1.5 size-3.5" />
          </a>
        </Button>
      </div>

      <form
        className="flex max-w-2xl flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        {keys.map((key) => (
          <div key={key} className="flex flex-col gap-1">
            <Label htmlFor={`slack-${key}`}>
              {SLACK_CREDENTIALS[key].label}
            </Label>
            <Input
              id={`slack-${key}`}
              type="password"
              reveal
              autoComplete="off"
              value={values[key] ?? ""}
              placeholder={SLACK_CREDENTIALS[key].placeholder}
              onChange={(value) =>
                setValues((prev) => ({ ...prev, [key]: value }))
              }
            />
            <Text small muted>
              {SLACK_CREDENTIALS[key].where}
            </Text>
          </div>
        ))}
        {save.error ? (
          <Text small className="text-destructive">
            Couldn't save the credentials. Try again.
          </Text>
        ) : null}
        <div className="flex gap-2">
          <Button
            type="submit"
            disabled={
              save.isPending ||
              !slack.env ||
              (onCancel ? !anyFilled : !requiredFilled)
            }
          >
            {save.isPending ? "Saving…" : "Save credentials"}
          </Button>
          {onCancel ? (
            <Button variant="tertiary" type="button" onClick={onCancel}>
              Cancel
            </Button>
          ) : null}
        </div>
      </form>
    </div>
  );
}
