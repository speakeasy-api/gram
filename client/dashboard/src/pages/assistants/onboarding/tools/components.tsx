import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { TextArea } from "@/components/ui/textarea";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { ToolCallMessagePartProps } from "@assistant-ui/react";
import { Icon } from "@speakeasy-api/moonshine";
import {
  Check,
  Copy,
  ExternalLink,
  Loader2,
  Shuffle,
  Sparkles,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useAssistantDraft } from "../useAssistantDraft";
import { PERSONALITIES, getPersonality } from "../personalities";
import { buildSlackManifest } from "../slackManifest";

type Status = ToolCallMessagePartProps["status"];

function isExecuting(status: Status) {
  return status.type === "running" || status.type === "requires-action";
}

function ToolCard({
  title,
  icon,
  children,
  tone = "default",
}: {
  title: string;
  icon?: React.ReactNode;
  children: React.ReactNode;
  tone?: "default" | "success" | "info";
}) {
  return (
    <div
      className={cn(
        "border-border bg-card my-3 max-w-2xl rounded-lg border shadow-sm",
        tone === "success" && "border-emerald-300/40 bg-emerald-50/30",
        tone === "info" && "border-sky-300/40 bg-sky-50/30",
      )}
    >
      <div className="border-border flex items-center gap-2 border-b px-5 py-3">
        {icon}
        <Type variant="body" className="font-medium">
          {title}
        </Type>
      </div>
      <div className="px-5 py-4">{children}</div>
    </div>
  );
}

type SecretKey = {
  name: string;
  label?: string;
  description?: string;
  sensitive?: boolean;
  placeholder?: string;
};

type SecretsArgs = {
  reason?: string;
  keys: SecretKey[];
};

type SecretsResult = {
  ok?: boolean;
  saved?: boolean;
  environment_slug?: string;
  saved_keys?: string[];
  declared_keys?: string[];
  cancelled?: boolean;
  error?: string;
};

export function RequestEnvironmentSecretsComponent({
  args,
  status,
  result,
  toolCallId,
}: ToolCallMessagePartProps) {
  const draft = useAssistantDraft();
  const a = (args ?? {}) as Partial<SecretsArgs>;
  const keys = Array.isArray(a.keys) ? a.keys : [];
  const envSlug = draft.assistantEnv?.slug ?? "";
  const reason = a.reason;

  const [values, setValues] = useState<Record<string, string>>(() =>
    Object.fromEntries(keys.map((k) => [k.name, ""])),
  );
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const isPending = isExecuting(status);
  const settled = !isPending;
  const r = result as SecretsResult | undefined;

  useEffect(() => {
    if (!isPending) return;
    return () => {
      draft.resolvePending(toolCallId, {
        success: false,
        cancelled: true,
      });
    };
  }, [draft, toolCallId, isPending]);

  if (settled && r?.saved) {
    const savedKeys = r.saved_keys ?? [];
    return (
      <ToolCard
        title="Environment secrets saved"
        tone="success"
        icon={<Check className="text-emerald-600" size={16} />}
      >
        <Type small muted>
          Saved to <code>{r.environment_slug}</code>:{" "}
          {savedKeys.length === 0 ? (
            <em>no values provided; keys declared as empty.</em>
          ) : (
            savedKeys.map((k) => (
              <code key={k} className="bg-muted mr-1 rounded px-1.5 py-0.5">
                {k}
              </code>
            ))
          )}
        </Type>
      </ToolCard>
    );
  }

  if (settled && r?.cancelled) {
    return (
      <ToolCard title="Environment secrets — skipped">
        <Type small muted>
          You can add these later from the Environments page.
        </Type>
      </ToolCard>
    );
  }

  if (settled) {
    return (
      <ToolCard title="Environment secrets — error">
        <Type small className="text-red-600">
          {r?.error ?? "Form was closed without saving."}
        </Type>
      </ToolCard>
    );
  }

  const anyFilled = keys.some((k) => (values[k.name] ?? "").length > 0);

  const submit = async () => {
    setSubmitting(true);
    setError(null);
    try {
      const ok = draft.resolvePending(toolCallId, {
        success: true,
        values,
      });
      if (!ok) {
        setError("This form is no longer connected to the assistant.");
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to save");
    } finally {
      setSubmitting(false);
    }
  };

  const cancel = () => {
    draft.resolvePending(toolCallId, {
      success: false,
      cancelled: true,
    });
  };

  return (
    <ToolCard
      title={envSlug ? `Add secrets to ${envSlug}` : "Add secrets"}
      icon={<Icon name="key-round" className="text-muted-foreground h-4 w-4" />}
    >
      {reason && (
        <Type small muted className="mb-3">
          {reason}
        </Type>
      )}
      <div className="space-y-3">
        {keys.map((k) => (
          <div key={k.name}>
            <Label className="mb-1 block text-xs">
              {k.label ?? k.name}{" "}
              <code className="text-muted-foreground">{k.name}</code>
            </Label>
            {k.description && (
              <Type small muted className="mb-1">
                {k.description}
              </Type>
            )}
            <Input
              type={k.sensitive ? "password" : "text"}
              value={values[k.name] ?? ""}
              onChange={(v) => setValues((prev) => ({ ...prev, [k.name]: v }))}
              placeholder={k.placeholder ?? ""}
            />
          </div>
        ))}
      </div>
      {error && (
        <Type small className="mt-2 text-red-600">
          {error}
        </Type>
      )}
      <div className="mt-4 flex justify-end gap-2">
        <Button variant="ghost" onClick={cancel} disabled={submitting}>
          Skip
        </Button>
        <Button onClick={submit} disabled={!anyFilled || submitting}>
          {submitting ? (
            <Loader2 className="mr-2 h-4 w-4 animate-spin" />
          ) : null}
          Save secrets
        </Button>
      </div>
    </ToolCard>
  );
}

type WebhookArgs = {
  webhook_url: string;
  trigger_name?: string;
  instructions?: string;
};

export function ShowWebhookUrlComponent({ args }: ToolCallMessagePartProps) {
  const a = (args ?? {}) as Partial<WebhookArgs>;
  const url = a.webhook_url ?? "";
  const [copied, setCopied] = useState(false);

  const copy = async () => {
    if (!url) return;
    await navigator.clipboard.writeText(url);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  return (
    <ToolCard
      title={a.trigger_name ? `Webhook for ${a.trigger_name}` : "Webhook URL"}
      tone="info"
      icon={<Icon name="webhook" className="text-muted-foreground h-4 w-4" />}
    >
      {a.instructions && (
        <Type small muted className="mb-3 whitespace-pre-line">
          {a.instructions}
        </Type>
      )}
      <div className="border-border bg-muted/30 flex items-center gap-2 rounded-md border px-3 py-2">
        <code className="flex-1 truncate font-mono text-xs">{url}</code>
        <Button size="sm" variant="ghost" onClick={copy}>
          {copied ? (
            <Check className="h-3.5 w-3.5" />
          ) : (
            <Copy className="h-3.5 w-3.5" />
          )}
        </Button>
      </div>
    </ToolCard>
  );
}

type SlackAppArgs = {
  app_name?: string;
  workspace_hint?: string;
  bot_scopes?: string[];
  bot_events?: string[];
  webhook_url?: string;
};

type SlackAppGuideResult = {
  success: boolean;
  installed?: boolean;
  cancelled?: boolean;
};

export function ShowSlackAppGuideComponent({
  args,
  status,
  result,
  toolCallId,
}: ToolCallMessagePartProps) {
  const a = (args ?? {}) as Partial<SlackAppArgs>;
  const draft = useAssistantDraft();
  const assistantName = draft.assistant?.name;

  const manifestResult = useMemo(
    () =>
      buildSlackManifest({
        appName: a.app_name ?? assistantName ?? "Gram Assistant",
        webhookUrl: a.webhook_url,
        extraScopes: a.bot_scopes,
        extraBotEvents: a.bot_events,
      }),
    [a.app_name, a.webhook_url, a.bot_scopes, a.bot_events, assistantName],
  );

  const webhookLive = !!a.webhook_url;
  const { deepLink, scopes, userScopes, botEvents } = manifestResult;

  const isPending = isExecuting(status);
  const settled = !isPending;
  const r = result as SlackAppGuideResult | undefined;

  useEffect(() => {
    if (!isPending) return;
    return () => {
      draft.resolvePending(toolCallId, {
        success: false,
        cancelled: true,
      });
    };
  }, [draft, toolCallId, isPending]);

  if (settled && r?.installed) {
    return (
      <ToolCard
        title="Slack connection installed"
        tone="success"
        icon={<Check className="text-emerald-600" size={16} />}
      >
        <Type small muted>
          Next: paste your tokens.
        </Type>
      </ToolCard>
    );
  }

  if (settled && r?.cancelled) {
    return (
      <ToolCard title="Slack install — skipped">
        <Type small muted>
          You can come back to this anytime — just ask me to retry the Slack
          setup.
        </Type>
      </ToolCard>
    );
  }

  const markInstalled = () => {
    draft.resolvePending(toolCallId, { success: true, installed: true });
  };
  const skip = () => {
    draft.resolvePending(toolCallId, { success: false, cancelled: true });
  };

  const steps: React.ReactNode[] = [
    <>
      Click <strong>Open Slack</strong> below. A new tab opens with the setup
      already filled in for you.
    </>,
    <>
      In Slack, pick the workspace this assistant should live in, then click{" "}
      <strong>Create</strong>.
    </>,
    <>
      In Slack's left sidebar, click <strong>Install App</strong>, then{" "}
      <strong>Install to Workspace</strong>, then approve. Slack will only hand
      out a login token <em>after</em> this step — skipping it is the most
      common reason setup gets stuck.
    </>,
    webhookLive ? (
      <>
        <span className="text-amber-600 dark:text-amber-400">
          Easy to miss, but required.
        </span>{" "}
        In Slack's left sidebar, click <strong>Event Subscriptions</strong>.
        Next to the request URL we filled in, click <strong>Retry</strong>, then
        click <strong>Save Changes</strong> in the bottom-right. Without this,
        Slack won't deliver any messages to your assistant and it will look
        broken.
      </>
    ) : null,
    <>
      Come back here and click <strong>I'm done</strong> below. I'll ask for
      your tokens next.
    </>,
  ].filter(Boolean);

  return (
    <ToolCard
      title="Install your Slack connection"
      tone="info"
      icon={<Icon name="bot" className="text-muted-foreground h-4 w-4" />}
    >
      <p className="text-muted-foreground mb-4 text-sm leading-relaxed">
        Slack calls each integration an "app" in their dashboard — same thing.
        I've pre-filled the setup so you just need to install it in your
        workspace
        {webhookLive ? " and tell Slack our webhook is ready" : ""}.
      </p>

      <ol className="space-y-3 text-sm leading-relaxed">
        {steps.map((step, i) => (
          <li key={i} className="flex gap-3">
            <span className="text-muted-foreground shrink-0 tabular-nums">
              {i + 1}.
            </span>
            <div className="flex-1">{step}</div>
          </li>
        ))}
      </ol>

      {(scopes.length > 0 || userScopes.length > 0 || botEvents.length > 0) && (
        <details className="border-border bg-muted/30 mt-4 rounded-md border p-3 text-xs">
          <summary className="text-muted-foreground cursor-pointer font-medium">
            What's pre-filled (advanced)
          </summary>
          <div className="mt-2 space-y-1.5">
            {scopes.length > 0 && (
              <div>
                <span className="text-muted-foreground">Bot permissions: </span>
                <span className="font-mono">{scopes.join(", ")}</span>
              </div>
            )}
            {userScopes.length > 0 && (
              <div>
                <span className="text-muted-foreground">
                  User permissions:{" "}
                </span>
                <span className="font-mono">{userScopes.join(", ")}</span>
              </div>
            )}
            {botEvents.length > 0 && (
              <div>
                <span className="text-muted-foreground">Listens for: </span>
                <span className="font-mono">{botEvents.join(", ")}</span>
              </div>
            )}
          </div>
        </details>
      )}

      <div className="mt-4 flex flex-wrap items-center justify-between gap-2">
        <Button asChild>
          <a href={deepLink} target="_blank" rel="noopener noreferrer">
            <ExternalLink className="mr-2 h-4 w-4" />
            Open Slack
          </a>
        </Button>
        <div className="flex gap-2">
          <Button variant="ghost" onClick={skip}>
            Skip
          </Button>
          <Button onClick={markInstalled}>I'm done</Button>
        </div>
      </div>
    </ToolCard>
  );
}

type ProposeIdentityArgs = {
  goal?: string;
  name_suggestions: string[];
};

type PersonalityChoice =
  | {
      kind: "prebuilt";
      prebuilt: {
        slug: string;
        title: string;
        summary: string;
        instructions: string;
      };
    }
  | { kind: "custom_text"; custom_text: string }
  | { kind: "generate"; describe: string }
  | { kind: "random" };

type IdentityResult = {
  success: boolean;
  cancelled?: boolean;
  name?: string;
  personality?: PersonalityChoice;
};

type PersonalityMode = "prebuilt" | "generate" | "custom" | "random";

export function ProposeIdentityComponent({
  args,
  status,
  result,
  toolCallId,
}: ToolCallMessagePartProps) {
  const draft = useAssistantDraft();
  const a = (args ?? {}) as Partial<ProposeIdentityArgs>;
  const suggestions = useMemo(
    () => (Array.isArray(a.name_suggestions) ? a.name_suggestions : []),
    [a.name_suggestions],
  );
  const goal = a.goal;

  const [name, setName] = useState<string>(suggestions[0] ?? "");
  const [mode, setMode] = useState<PersonalityMode>("prebuilt");
  const [prebuiltSlug, setPrebuiltSlug] = useState<string>(
    PERSONALITIES[0]?.slug ?? "",
  );
  const [describeText, setDescribeText] = useState<string>("");
  const [customText, setCustomText] = useState<string>("");

  const isPending = isExecuting(status);
  const settled = !isPending;
  const r = result as { ok?: boolean; name?: string } | undefined;

  useEffect(() => {
    if (!isPending) return;
    return () => {
      draft.resolvePending(toolCallId, {
        success: false,
        cancelled: true,
      } satisfies IdentityResult);
    };
  }, [draft, toolCallId, isPending]);

  if (settled && r?.ok) {
    return (
      <ToolCard
        title="Identity set"
        tone="success"
        icon={<Check className="text-emerald-600" size={16} />}
      >
        <Type small muted>
          Name: <code className="bg-muted rounded px-1.5 py-0.5">{r.name}</code>
        </Type>
      </ToolCard>
    );
  }

  if (settled) {
    return (
      <ToolCard title="Identity — skipped">
        <Type small muted>
          No identity selected. You can set one from the draft panel anytime.
        </Type>
      </ToolCard>
    );
  }

  const trimmedName = name.trim();
  const canSubmit =
    trimmedName.length > 0 &&
    ((mode === "prebuilt" && prebuiltSlug.length > 0) ||
      (mode === "generate" && describeText.trim().length > 0) ||
      (mode === "custom" && customText.trim().length > 0) ||
      mode === "random");

  const submit = () => {
    let personality: PersonalityChoice | undefined;
    if (mode === "prebuilt") {
      const p = getPersonality(prebuiltSlug);
      if (!p) return;
      personality = {
        kind: "prebuilt",
        prebuilt: {
          slug: p.slug,
          title: p.title,
          summary: p.summary,
          instructions: p.instructions,
        },
      };
    } else if (mode === "generate") {
      personality = { kind: "generate", describe: describeText.trim() };
    } else if (mode === "custom") {
      personality = { kind: "custom_text", custom_text: customText.trim() };
    } else {
      personality = { kind: "random" };
    }
    draft.resolvePending(toolCallId, {
      success: true,
      name: trimmedName,
      personality,
    } satisfies IdentityResult);
  };

  const cancel = () => {
    draft.resolvePending(toolCallId, {
      success: false,
      cancelled: true,
    } satisfies IdentityResult);
  };

  return (
    <ToolCard
      title="Name and personality"
      icon={<Sparkles className="text-muted-foreground h-4 w-4" />}
    >
      {goal && (
        <Type small muted className="mb-3">
          Based on: {goal}
        </Type>
      )}

      <div className="mb-4">
        <Label className="mb-1 block text-xs font-medium">Name</Label>
        {suggestions.length > 0 && (
          <div className="mb-2 flex flex-wrap gap-1.5">
            {suggestions.map((s) => (
              <button
                key={s}
                type="button"
                onClick={() => setName(s)}
                className={cn(
                  "border-border hover:bg-muted rounded-full border px-2.5 py-1 text-xs transition-colors",
                  name === s &&
                    "bg-primary text-primary-foreground border-primary",
                )}
              >
                {s}
              </button>
            ))}
          </div>
        )}
        <Input
          value={name}
          onChange={setName}
          placeholder="Pick a suggestion or type your own"
        />
      </div>

      <div>
        <Label className="mb-2 block text-xs font-medium">Personality</Label>
        <RadioGroup
          value={mode}
          onValueChange={(v) => setMode(v as PersonalityMode)}
          className="gap-2"
        >
          <div className="border-border rounded-md border p-3">
            <div className="flex items-start gap-2">
              <RadioGroupItem
                value="prebuilt"
                id="identity-prebuilt"
                className="mt-1"
              />
              <Label
                htmlFor="identity-prebuilt"
                className="flex-1 cursor-pointer flex-col items-start gap-0"
              >
                <Type small className="font-medium">
                  Pick a preset
                </Type>
              </Label>
            </div>
            {mode === "prebuilt" && (
              <div className="mt-3 grid grid-cols-1 gap-1.5 sm:grid-cols-2">
                {PERSONALITIES.map((p) => (
                  <button
                    key={p.slug}
                    type="button"
                    onClick={() => setPrebuiltSlug(p.slug)}
                    className={cn(
                      "border-border hover:bg-muted rounded-md border p-2 text-left transition-colors",
                      prebuiltSlug === p.slug && "border-primary bg-primary/5",
                    )}
                  >
                    <Type small className="font-medium">
                      {p.title}
                    </Type>
                    <Type small muted className="mt-0.5">
                      {p.summary}
                    </Type>
                  </button>
                ))}
              </div>
            )}
          </div>

          <div className="border-border rounded-md border p-3">
            <div className="flex items-start gap-2">
              <RadioGroupItem
                value="generate"
                id="identity-generate"
                className="mt-1"
              />
              <Label
                htmlFor="identity-generate"
                className="flex-1 cursor-pointer flex-col items-start gap-0"
              >
                <Type small className="font-medium">
                  Describe it in your own words
                </Type>
                <Type small muted className="mt-0.5">
                  A sentence or two — we'll expand it into a full personality.
                </Type>
              </Label>
            </div>
            {mode === "generate" && (
              <TextArea
                value={describeText}
                onChange={setDescribeText}
                placeholder="e.g. friendly but precise; uses emoji sparingly; signs off as 'yours truly'"
                rows={3}
                className="mt-2"
              />
            )}
          </div>

          <div className="border-border rounded-md border p-3">
            <div className="flex items-start gap-2">
              <RadioGroupItem
                value="custom"
                id="identity-custom"
                className="mt-1"
              />
              <Label
                htmlFor="identity-custom"
                className="flex-1 cursor-pointer flex-col items-start gap-0"
              >
                <Type small className="font-medium">
                  Paste full instructions
                </Type>
                <Type small muted className="mt-0.5">
                  Use as-is. You can always edit later.
                </Type>
              </Label>
            </div>
            {mode === "custom" && (
              <TextArea
                value={customText}
                onChange={setCustomText}
                placeholder="Full system prompt…"
                rows={5}
                className="mt-2 font-mono text-xs"
              />
            )}
          </div>

          <div className="border-border rounded-md border p-3">
            <div className="flex items-start gap-2">
              <RadioGroupItem
                value="random"
                id="identity-random"
                className="mt-1"
              />
              <Label
                htmlFor="identity-random"
                className="flex-1 cursor-pointer flex-col items-start gap-0"
              >
                <Type small className="font-medium">
                  <Shuffle className="mr-1 inline h-3.5 w-3.5" />
                  Surprise me
                </Type>
                <Type small muted className="mt-0.5">
                  Generate a fresh, unexpected personality.
                </Type>
              </Label>
            </div>
          </div>
        </RadioGroup>
      </div>

      <div className="mt-4 flex justify-end gap-2">
        <Button variant="ghost" onClick={cancel}>
          Skip
        </Button>
        <Button onClick={submit} disabled={!canSubmit}>
          Save
        </Button>
      </div>
    </ToolCard>
  );
}

export function NoticeOnUnmount({
  toolCallId,
  status,
}: ToolCallMessagePartProps) {
  const draft = useAssistantDraft();
  const isPending = isExecuting(status);
  useEffect(() => {
    if (!isPending) return;
    return () => {
      draft.resolvePending(toolCallId, {
        success: false,
        cancelled: true,
        reason: "Component unmounted before user submitted.",
      });
    };
  }, [draft, toolCallId, isPending]);
  return null;
}
