import { CodeBlock } from "@/components/code";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import {
  CLAUDE_CODE_EXACT_NAME_NOTE,
  claudeCodeSettingsJson,
} from "@/lib/claude-code-marketplace";
import { cn } from "@/lib/utils";
import { ChevronRight } from "lucide-react";
import { useState } from "react";

const INLINE_CODE_CLASS = "bg-muted px-1 py-0.5 text-xs";

type SettingsScope = "user" | "managed";

function InlineCode({ children }: { children: string }): React.JSX.Element {
  return <code className={INLINE_CODE_CLASS}>{children}</code>;
}

/** A collapsed "how do I…" answer under a snippet. */
function HelpDisclosure({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}): React.JSX.Element {
  const [open, setOpen] = useState(false);
  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger asChild>
        <button
          type="button"
          className="text-muted-foreground hover:text-foreground flex items-center gap-1 text-xs font-medium"
        >
          <ChevronRight
            aria-hidden
            className={cn("size-3.5 transition-transform", open && "rotate-90")}
          />
          {label}
        </button>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <ul className="text-muted-foreground mt-2 list-disc space-y-1 pl-5 text-xs leading-relaxed">
          {children}
        </ul>
      </CollapsibleContent>
    </Collapsible>
  );
}

function UserSettingsHelp(): React.JSX.Element {
  return (
    <HelpDisclosure label="Already have a settings.json?">
      <li>
        No file yet: create <InlineCode>~/.claude/settings.json</InlineCode>{" "}
        with the snippet.
      </li>
      <li>
        Otherwise add the marketplace entry inside{" "}
        <InlineCode>extraKnownMarketplaces</InlineCode> and the plugin line
        inside <InlineCode>enabledPlugins</InlineCode>.
      </li>
    </HelpDisclosure>
  );
}

function ManagedSettingsHelp(): React.JSX.Element {
  return (
    <HelpDisclosure label="Where are managed settings?">
      <li>
        Claude admin console: Admin settings → Claude Code → Managed settings
        (Team or Enterprise Owners).
      </li>
      <li>Your MDM, as a managed policy.</li>
      <li>
        <InlineCode>managed-settings.json</InlineCode> in the system config
        directory.
      </li>
    </HelpDisclosure>
  );
}

const SCOPE_OPTIONS: {
  value: SettingsScope;
  title: string;
  tagline: React.ReactNode;
  help: React.ReactNode;
}[] = [
  {
    value: "user",
    title: "Just me",
    tagline: <InlineCode>~/.claude/settings.json</InlineCode>,
    help: <UserSettingsHelp />,
  },
  {
    value: "managed",
    title: "My organization",
    tagline: "Managed settings, applied to everyone",
    help: <ManagedSettingsHelp />,
  },
];

/**
 * The Claude Code install every Speakeasy surface offers: one settings
 * snippet that registers the marketplace with autoUpdate on and enables the
 * plugins, so there is no CLI step. A radio accordion picks where it goes;
 * the chosen option expands with the snippet and a collapsed answer for the
 * common follow-up question.
 */
export function ClaudeCodeSettingsInstall({
  marketplaceName,
  marketplaceUrl,
  plugins,
  title = "Add to your Claude Code settings",
  secretUrl = false,
  onCopy,
  nextStep = (
    <>
      Restart Claude Code. To check it worked, open{" "}
      <InlineCode>/plugin</InlineCode> in a session.
    </>
  ),
}: {
  marketplaceName: string;
  marketplaceUrl: string;
  plugins: string[];
  /** Null when the caller already titles the section, e.g. an install step. */
  title?: string | null;
  /** The URL carries an access token, so the reader is told to keep it private. */
  secretUrl?: boolean;
  onCopy?: () => void;
  /** The line after the options; null when the caller shows its own next step. */
  nextStep?: React.ReactNode;
}): React.JSX.Element {
  const [scope, setScope] = useState<SettingsScope>("user");
  const snippet = claudeCodeSettingsJson({
    marketplaceName,
    marketplaceUrl,
    plugins,
  });
  const note = secretUrl
    ? `${CLAUDE_CODE_EXACT_NAME_NOTE} Keep the URL private.`
    : CLAUDE_CODE_EXACT_NAME_NOTE;

  return (
    <div className="min-w-0 space-y-3">
      {title ? <h3 className="text-sm font-semibold">{title}</h3> : null}
      <RadioCardGroup
        size="sm"
        value={scope}
        onValueChange={(next) => setScope(next as SettingsScope)}
        aria-label="Where to add the settings"
      >
        {SCOPE_OPTIONS.map((option) => (
          <RadioCard
            key={option.value}
            value={option.value}
            title={option.title}
          >
            {option.tagline}
            <Collapsible open={scope === option.value}>
              <CollapsibleContent className="mt-3 space-y-3">
                <CodeBlock
                  language="json"
                  className="bg-background"
                  onCopy={onCopy}
                >
                  {snippet}
                </CodeBlock>
                <p className="text-xs">{note}</p>
                {option.help}
              </CollapsibleContent>
            </Collapsible>
          </RadioCard>
        ))}
      </RadioCardGroup>
      {nextStep ? (
        <p className="text-muted-foreground text-sm">{nextStep}</p>
      ) : null}
    </div>
  );
}
