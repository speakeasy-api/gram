import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { cn } from "@/lib/utils";
import { ExternalLink, Plus } from "lucide-react";
import { useEffect, useState } from "react";
import { HookSourceIcon } from "./HookSourceIcon";

function ClaudeInstallContent() {
  return (
    <div className="space-y-6">
      <div>
        <h3 className="text-sm font-semibold mb-2">Test Yourself</h3>
        <p className="text-sm text-muted-foreground mb-4">
          Try Gram Hooks in your Claude Code instance:
        </p>
        <div className="bg-muted/50 rounded-lg p-4 font-mono text-sm space-y-2">
          <div className="flex items-center justify-between">
            <code>claude plugin marketplace add speakeasy-api/gram</code>
          </div>
          <div className="flex items-center justify-between">
            <code>claude plugin install gram-hooks@gram</code>
          </div>
        </div>
      </div>

      <div>
        <h3 className="text-sm font-semibold mb-2">Distribute to Your Team</h3>
        <p className="text-sm text-muted-foreground mb-4">
          Require your team to use Gram Hooks by configuring their Claude Code
          settings:
        </p>

        <div className="space-y-4">
          <div>
            <h4 className="text-xs font-medium text-muted-foreground mb-2">
              1. Require the marketplace
            </h4>
            <div className="bg-muted/50 rounded-lg p-4 font-mono text-sm">
              <code>
                {`{
  "pluginMarketplaces": {
    "required": ["speakeasy-api/gram"]
  }
}`}
              </code>
            </div>
          </div>

          <div>
            <h4 className="text-xs font-medium text-muted-foreground mb-2">
              2. Require the plugin
            </h4>
            <div className="bg-muted/50 rounded-lg p-4 font-mono text-sm">
              <code>
                {`{
  "plugins": {
    "required": ["gram-hooks@gram"]
  }
}`}
              </code>
            </div>
          </div>

          <Button variant="outline" size="sm" asChild>
            <a
              href="https://code.claude.com/docs/en/plugin-marketplaces#require-marketplaces-for-your-team"
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-2"
            >
              <ExternalLink className="size-4" />
              View Full Documentation
            </a>
          </Button>
        </div>
      </div>
    </div>
  );
}

function CursorInstallContent() {
  return (
    <div className="space-y-6">
      <div>
        <h3 className="text-sm font-semibold mb-2">1. Publish the Plugin</h3>
        <p className="text-sm text-muted-foreground mb-4">
          Add the Gram hooks plugin to your Cursor team marketplace and mark it
          as required so it auto-installs for all team members:
        </p>
        <div className="bg-muted/50 rounded-lg p-4 font-mono text-sm">
          <a
            href="https://cursor.com/dashboard/team-content"
            target="_blank"
            rel="noopener noreferrer"
            className="text-primary underline underline-offset-4 hover:text-primary/80"
          >
            cursor.com/dashboard/team-content
          </a>
        </div>
      </div>

      <div>
        <h3 className="text-sm font-semibold mb-2">2. Configure Credentials</h3>
        <p className="text-sm text-muted-foreground mb-4">
          In the Cursor team dashboard, add a{" "}
          <code className="text-xs bg-muted px-1 py-0.5 rounded">
            Session Start
          </code>{" "}
          hook that injects your Gram credentials. These are automatically
          passed to all subsequent hooks in the session.
        </p>
        <p className="text-sm text-muted-foreground mb-4">
          Go to{" "}
          <a
            href="https://cursor.com/dashboard/team-content?section=hooks"
            target="_blank"
            rel="noopener noreferrer"
            className="text-primary underline underline-offset-4 hover:text-primary/80"
          >
            cursor.com/dashboard/team-content
          </a>{" "}
          and create a new hook with:
        </p>
        <div className="bg-muted/50 rounded-lg p-4 text-sm space-y-3">
          <div className="flex items-baseline gap-2">
            <span className="text-muted-foreground font-medium shrink-0">
              Hook Name:
            </span>
            <code>Gram Hooks</code>
          </div>
          <div className="flex items-baseline gap-2">
            <span className="text-muted-foreground font-medium shrink-0">
              Hook Type:
            </span>
            <code>Command</code>
          </div>
          <div className="flex items-baseline gap-2">
            <span className="text-muted-foreground font-medium shrink-0">
              Hook Step:
            </span>
            <code>Session Start</code>
          </div>
          <div>
            <span className="text-muted-foreground font-medium">
              Script Content:
            </span>
            <div className="bg-background/50 rounded mt-1 p-3 font-mono text-xs whitespace-pre-wrap break-all overflow-x-auto">
              {`#!/bin/bash\necho '{"env":{"GRAM_API_KEY":"`}
              <span className="text-primary font-semibold">{`<YOUR_API_KEY>`}</span>
              {`","GRAM_PROJECT_SLUG":"`}
              <span className="text-primary font-semibold">{`<YOUR_PROJECT_SLUG>`}</span>
              {`"}}'`}
            </div>
          </div>
          <div className="flex items-baseline gap-2">
            <span className="text-muted-foreground font-medium shrink-0">
              Platforms:
            </span>
            <code>Mac, Linux</code>
          </div>
        </div>
        <p className="text-xs text-muted-foreground mt-2">
          Replace{" "}
          <code className="text-xs text-primary">{`<YOUR_API_KEY>`}</code> and{" "}
          <code className="text-xs text-primary">{`<YOUR_PROJECT_SLUG>`}</code>{" "}
          with your Gram credentials. Find your API key in your project's API
          Keys settings. This config syncs to all team members automatically.
        </p>
      </div>

      <div className="flex items-center gap-3">
        <Button variant="outline" size="sm" asChild>
          <a
            href="https://cursor.com/docs/plugins"
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-2"
          >
            <ExternalLink className="size-4" />
            Plugin Docs
          </a>
        </Button>
        <Button variant="outline" size="sm" asChild>
          <a
            href="https://cursor.com/docs/hooks"
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-2"
          >
            <ExternalLink className="size-4" />
            Hooks Docs
          </a>
        </Button>
      </div>
    </div>
  );
}

type Provider = "claude" | "cursor";

const providers: { id: Provider; label: string; source: string }[] = [
  { id: "claude", label: "Claude Code", source: "claude-code" },
  { id: "cursor", label: "Cursor", source: "cursor" },
];

export function HooksSetupDialog({
  open,
  onOpenChange,
  defaultProvider = "claude",
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  defaultProvider?: Provider;
}) {
  const [selected, setSelected] = useState<Provider>(defaultProvider);

  useEffect(() => {
    setSelected(defaultProvider);
  }, [defaultProvider]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="max-w-4xl">
        <Dialog.Header>
          <Dialog.Title>Setup Hooks</Dialog.Title>
        </Dialog.Header>

        <div className="flex gap-3 mb-6">
          {providers.map((p) => (
            <button
              key={p.id}
              onClick={() => setSelected(p.id)}
              className={cn(
                "flex items-center gap-2 px-3 py-2 rounded-md border text-sm font-medium transition-colors",
                selected === p.id
                  ? "border-primary bg-primary/5"
                  : "border-border hover:border-primary/50 hover:bg-muted/50",
              )}
            >
              <HookSourceIcon source={p.source} className="size-5" />
              {p.label}
            </button>
          ))}
        </div>

        {selected === "claude" && <ClaudeInstallContent />}
        {selected === "cursor" && <CursorInstallContent />}
      </Dialog.Content>
    </Dialog>
  );
}

export function HooksSetupButton() {
  const [open, setOpen] = useState(false);

  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        <Plus className="h-4 w-4" />
        Add provider
      </Button>
      <HooksSetupDialog open={open} onOpenChange={setOpen} />
    </>
  );
}
