import { Link } from "@/components/ui/Link";
import {
  CLAUDE_CODE_MARKETPLACE_AUTO_UPDATE_DOCS_URL,
  CLAUDE_CODE_REQUIRE_MARKETPLACE_DOCS_URL,
  claudeMarketplaceNameNoteParts,
  type ClaudeCodeNotePart,
} from "@/lib/claude-code-marketplace";
import { cn } from "@/lib/utils";
import { Info } from "lucide-react";

const INLINE_CODE_CLASS = "bg-muted px-1 py-0.5 text-xs";

function NoteLink({
  href,
  children,
}: {
  href: string;
  children: React.ReactNode;
}): React.JSX.Element {
  return (
    <Link
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      size="xs"
      iconSuffixName="external-link"
    >
      {children}
    </Link>
  );
}

function Note({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}): React.JSX.Element {
  return (
    <div
      className={cn(
        "text-muted-foreground mt-3 flex items-start gap-1.5 text-xs leading-relaxed",
        className,
      )}
    >
      <Info className="mt-0.5 size-3.5 shrink-0" />
      <span>{children}</span>
    </div>
  );
}

function NotePart({ part }: { part: ClaudeCodeNotePart }): React.ReactNode {
  if (typeof part === "string") return part;
  return <code className={INLINE_CODE_CLASS}>{part.code}</code>;
}

/**
 * Shown beside every Claude Code settings snippet that registers a Speakeasy
 * marketplace. Claude Code registers a marketplace under its marketplace.json
 * `name` and applies `autoUpdate` only from the extraKnownMarketplaces entry
 * keyed by that name, so an entry keyed by the GitHub repository name or the
 * pre-#2964 `<org>-gram` default leaves auto-update off without any error.
 */
export function ClaudeMarketplaceNameNote({
  marketplaceName,
  className,
}: {
  marketplaceName: string;
  className?: string;
}): React.JSX.Element {
  const parts = claudeMarketplaceNameNoteParts({
    code: marketplaceName,
    fallback: marketplaceName,
  });
  return (
    <Note className={className}>
      {parts.map((part, index) => (
        <NotePart key={index} part={part} />
      ))}{" "}
      <NoteLink href={CLAUDE_CODE_REQUIRE_MARKETPLACE_DOCS_URL}>
        Claude Code docs
      </NoteLink>
    </Note>
  );
}

/**
 * Shown beside every per-user `plugin marketplace add` command. A marketplace
 * added that way is registered with no `autoUpdate`, and auto-update is off by
 * default for third-party marketplaces, so users stay on the version they
 * first installed until they or an admin turn it on.
 */
export function ClaudeMarketplaceAutoUpdateNote({
  marketplaceName,
  className,
}: {
  marketplaceName?: string;
  className?: string;
}): React.JSX.Element {
  return (
    <Note className={className}>
      Auto-update is off for a marketplace added this way: open{" "}
      <code className={INLINE_CODE_CLASS}>/plugin</code> → Marketplaces, select{" "}
      {marketplaceName ? (
        <code className={INLINE_CODE_CLASS}>{marketplaceName}</code>
      ) : (
        "this marketplace"
      )}
      , and choose Enable auto-update, or ask your admin to enforce it in
      managed settings.{" "}
      <NoteLink href={CLAUDE_CODE_MARKETPLACE_AUTO_UPDATE_DOCS_URL}>
        Turn on auto-update
      </NoteLink>
    </Note>
  );
}
