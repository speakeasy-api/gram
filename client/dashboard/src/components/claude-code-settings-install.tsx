import { CodeBlock } from "@/components/code";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { Link } from "@/components/ui/Link";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import {
  CLAUDE_CODE_EXACT_NAME_NOTE,
  CLAUDE_CODE_REQUIRE_MARKETPLACE_DOCS_URL,
  claudeCodeSettingsJson,
} from "@/lib/claude-code-marketplace";
import { cn } from "@/lib/utils";
import { useMarketplaceSettings } from "@gram/client/react-query/marketplaceSettings";
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
      <li>Otherwise merge each top-level key into the one in your file.</li>
    </HelpDisclosure>
  );
}

function ManagedSettingsHelp(): React.JSX.Element {
  return (
    <HelpDisclosure label="Where are managed settings?">
      <li>
        Claude admin console: Admin settings → Claude Code → Managed settings
        (Team or Enterprise Owners). Not fetched with a custom{" "}
        <InlineCode>ANTHROPIC_BASE_URL</InlineCode> or a third-party provider.
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
    value: "managed",
    title: "My organization",
    tagline: "Managed settings, applied to everyone",
    help: <ManagedSettingsHelp />,
  },
  {
    value: "user",
    title: "Just me",
    tagline: <InlineCode>~/.claude/settings.json</InlineCode>,
    help: <UserSettingsHelp />,
  },
];

type InstallProps = {
  marketplaceUrl: string;
  plugins: string[];
  /** Null when the caller already titles the section, e.g. an install step. */
  title?: string | null;
  /** The URL carries an access token, so the reader is told to keep it private. */
  secretUrl?: boolean;
  onCopy?: () => void;
  /** The line after the options; null when the caller shows its own next step. */
  nextStep?: React.ReactNode;
};

/**
 * The Claude Code install every Speakeasy surface offers: one settings
 * snippet that registers the marketplace with autoUpdate on and enables the
 * plugins, so there is no CLI step. Pass `marketplaceName` when the caller
 * already has it (or for a fixed marketplace, like Platform MCP's public
 * one); otherwise the project's published marketplace.json name is read from
 * marketplace settings.
 */
export function ClaudeCodeSettingsInstall({
  marketplaceName,
  ...props
}: InstallProps & { marketplaceName?: string }): React.JSX.Element {
  if (marketplaceName !== undefined) {
    return <SettingsInstall marketplaceName={marketplaceName} {...props} />;
  }
  return <ProjectSettingsInstall {...props} />;
}

function ProjectSettingsInstall(props: InstallProps): React.JSX.Element {
  const { data, isPending } = useMarketplaceSettings(undefined, undefined, {
    throwOnError: false,
  });
  return (
    <SettingsInstall
      marketplaceName={data?.effectiveName}
      loading={!!isPending}
      {...props}
    />
  );
}

/**
 * A radio accordion picks where the snippet goes, organization first and
 * selected by default; the chosen option reveals the snippet and a collapsed
 * answer for the common follow-up question. The reveal uses RadioCard's
 * detail slot, so the radio's accessible description stays the tagline.
 */
function SettingsInstall({
  marketplaceName,
  loading = false,
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
}: InstallProps & {
  marketplaceName: string | undefined;
  loading?: boolean;
}): React.JSX.Element {
  const [scope, setScope] = useState<SettingsScope>("managed");
  const note = secretUrl
    ? `${CLAUDE_CODE_EXACT_NAME_NOTE} Keep the URL private.`
    : CLAUDE_CODE_EXACT_NAME_NOTE;

  return (
    <div className="min-w-0 space-y-3">
      {title ? <h3 className="text-sm font-semibold">{title}</h3> : null}
      {marketplaceName ? (
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
              detail={
                scope === option.value ? (
                  <div className="space-y-3">
                    <CodeBlock language="json" onCopy={onCopy}>
                      {claudeCodeSettingsJson({
                        marketplaceName,
                        marketplaceUrl,
                        plugins,
                      })}
                    </CodeBlock>
                    <p className="text-xs">
                      {note}{" "}
                      <Link
                        href={CLAUDE_CODE_REQUIRE_MARKETPLACE_DOCS_URL}
                        target="_blank"
                        rel="noopener noreferrer"
                        size="xs"
                        iconSuffixName="external-link"
                      >
                        Docs
                      </Link>
                    </p>
                    {option.help}
                  </div>
                ) : null
              }
            >
              {option.tagline}
            </RadioCard>
          ))}
        </RadioCardGroup>
      ) : (
        <p className="text-muted-foreground text-sm italic">
          {loading
            ? "Loading the marketplace name…"
            : "Couldn't load the marketplace name. Reload to try again."}
        </p>
      )}
      {marketplaceName && nextStep ? (
        <p className="text-muted-foreground text-sm">{nextStep}</p>
      ) : null}
    </div>
  );
}
