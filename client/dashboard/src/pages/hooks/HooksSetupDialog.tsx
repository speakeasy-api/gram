import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { ExternalLink, Plus } from "lucide-react";
import { useEffect, useState } from "react";
import { HookSourceIcon } from "./HookSourceIcon";

function ClaudeInstallContent() {
  return (
    <div className="space-y-6">
      <div>
        <h3 className="mb-2 text-sm font-semibold">Test Yourself</h3>
        <p className="text-muted-foreground mb-4 text-sm">
          Try Speakeasy Hooks in your Claude Code instance:
        </p>
        <div className="bg-muted/50 space-y-2 rounded-lg p-4 font-mono text-sm">
          <div className="flex items-center justify-between">
            <code>claude plugin marketplace add speakeasy-api/gram</code>
          </div>
          <div className="flex items-center justify-between">
            <code>claude plugin install gram-hooks@gram</code>
          </div>
        </div>
      </div>

      <div>
        <h3 className="mb-2 text-sm font-semibold">Distribute to Your Team</h3>
        <p className="text-muted-foreground mb-4 text-sm">
          Require your team to use Speakeasy Hooks by configuring their Claude
          Code settings:
        </p>

        <div className="space-y-4">
          <div>
            <h4 className="text-muted-foreground mb-2 text-xs font-medium">
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
            <h4 className="text-muted-foreground mb-2 text-xs font-medium">
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
        <h3 className="mb-2 text-sm font-semibold">1. Publish the Plugin</h3>
        <p className="text-muted-foreground mb-4 text-sm">
          Add the Speakeasy hooks plugin to your Cursor team marketplace and
          mark it as required so it auto-installs for all team members:
        </p>
        <div className="bg-muted/50 rounded-lg p-4 font-mono text-sm">
          <a
            href="https://cursor.com/dashboard/team-content"
            target="_blank"
            rel="noopener noreferrer"
            className="text-primary hover:text-primary/80 underline underline-offset-4"
          >
            cursor.com/dashboard/team-content
          </a>
        </div>
      </div>

      <div>
        <h3 className="mb-2 text-sm font-semibold">2. Configure Credentials</h3>
        <p className="text-muted-foreground mb-4 text-sm">
          In the Cursor team dashboard, add a{" "}
          <code className="bg-muted rounded px-1 py-0.5 text-xs">
            Session Start
          </code>{" "}
          hook that injects your Speakeasy credentials. These are automatically
          passed to all subsequent hooks in the session.
        </p>
        <p className="text-muted-foreground mb-4 text-sm">
          Go to{" "}
          <a
            href="https://cursor.com/dashboard/team-content?section=hooks"
            target="_blank"
            rel="noopener noreferrer"
            className="text-primary hover:text-primary/80 underline underline-offset-4"
          >
            cursor.com/dashboard/team-content
          </a>{" "}
          and create a new hook with:
        </p>
        <div className="bg-muted/50 space-y-3 rounded-lg p-4 text-sm">
          <div className="flex items-baseline gap-2">
            <span className="text-muted-foreground shrink-0 font-medium">
              Hook Name:
            </span>
            <code>Speakeasy Hooks</code>
          </div>
          <div className="flex items-baseline gap-2">
            <span className="text-muted-foreground shrink-0 font-medium">
              Hook Type:
            </span>
            <code>Command</code>
          </div>
          <div className="flex items-baseline gap-2">
            <span className="text-muted-foreground shrink-0 font-medium">
              Hook Step:
            </span>
            <code>Session Start</code>
          </div>
          <div>
            <span className="text-muted-foreground font-medium">
              Script Content:
            </span>
            <div className="bg-background/50 mt-1 overflow-x-auto rounded p-3 font-mono text-xs break-all whitespace-pre-wrap">
              {`#!/bin/bash\necho '{"env":{"GRAM_API_KEY":"`}
              <span className="text-primary font-semibold">{`<YOUR_API_KEY>`}</span>
              {`","GRAM_PROJECT_SLUG":"`}
              <span className="text-primary font-semibold">{`<YOUR_PROJECT_SLUG>`}</span>
              {`"}}'`}
            </div>
          </div>
          <div className="flex items-baseline gap-2">
            <span className="text-muted-foreground shrink-0 font-medium">
              Platforms:
            </span>
            <code>Mac, Linux</code>
          </div>
        </div>
        <p className="text-muted-foreground mt-2 text-xs">
          Replace{" "}
          <code className="text-primary text-xs">{`<YOUR_API_KEY>`}</code> and{" "}
          <code className="text-primary text-xs">{`<YOUR_PROJECT_SLUG>`}</code>{" "}
          with your Speakeasy credentials. Find your API key in your project's
          API Keys settings. This config syncs to all team members
          automatically.
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

type Provider =
  | "claude"
  | "cursor"
  | "codex"
  | "copilot"
  | "gemini"
  | "glean"
  | "bedrock";

const providers: {
  id: Provider;
  label: string;
  source: string;
  available: boolean;
}[] = [
  {
    id: "claude",
    label: "Claude Code",
    source: "claude-code",
    available: true,
  },
  { id: "cursor", label: "Cursor", source: "cursor", available: true },
  { id: "codex", label: "Codex", source: "codex", available: false },
  {
    id: "copilot",
    label: "Copilot",
    source: "copilot",
    available: false,
  },
  { id: "gemini", label: "Gemini", source: "gemini", available: false },
  { id: "glean", label: "Glean", source: "glean", available: false },
  {
    id: "bedrock",
    label: "AWS Bedrock",
    source: "aws-bedrock",
    available: false,
  },
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

        <div className="mb-6 flex flex-wrap gap-3">
          <TooltipProvider>
            {providers.map((p) => {
              const button = (
                <button
                  key={p.id}
                  onClick={() => p.available && setSelected(p.id)}
                  disabled={!p.available}
                  className={cn(
                    "relative flex items-center gap-2 rounded-md border px-3 py-2 text-sm font-medium transition-colors",
                    selected === p.id
                      ? "border-primary bg-primary/5"
                      : "border-border hover:border-primary/50 hover:bg-muted/50",
                    !p.available &&
                      "hover:border-border cursor-not-allowed opacity-50 hover:bg-transparent",
                  )}
                >
                  <HookSourceIcon source={p.source} className="size-5" />
                  {p.label}
                  {!p.available && (
                    <span className="text-muted-foreground ml-1 text-[10px] tracking-wide uppercase">
                      Soon
                    </span>
                  )}
                </button>
              );

              if (!p.available) {
                return (
                  <Tooltip key={p.id}>
                    <TooltipTrigger asChild>{button}</TooltipTrigger>
                    <TooltipContent>
                      <p>Coming soon</p>
                    </TooltipContent>
                  </Tooltip>
                );
              }

              return button;
            })}
          </TooltipProvider>
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
