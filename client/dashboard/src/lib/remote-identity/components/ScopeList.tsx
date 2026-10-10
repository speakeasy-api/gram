import { Badge } from "@/components/ui/Badge";
import {
  MoreToggle,
  useCollapsedPreview,
} from "@/components/ui/collapsible-preview";
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/HoverCard";
import { Text } from "@/components/ui/Text";
import { useId, useLayoutEffect, useMemo, useRef, useState } from "react";
import { sharedScopePrefix, shortScope } from "../model/scopePrefix";

// Enough to show a typical provider whole; Google and Microsoft advertise
// hundreds, which collapse behind "+N more".
const PREVIEW_COUNT = 12;

// Chip classes shared by the chip and the hover card laid over it, so the
// full scope lands exactly on the short one.
const CHIP_TEXT = "font-mono text-xs leading-none tracking-normal normal-case";

/**
 * A read-only list of scopes as chips.
 *
 * With `maxLines`, the list is clipped to that many lines and ends in a plain
 * "and N more"; the full list lives on the provider's own page. Without it,
 * a long list collapses behind a "+N more" toggle.
 */
export function ScopeList({
  scopes: listed,
  maxLines,
}: {
  scopes: string[] | null | undefined;
  maxLines?: number;
}): JSX.Element {
  // Providers occasionally list a scope twice; each chip is keyed by scope.
  const scopes = useMemo(() => [...new Set(listed ?? [])], [listed]);
  const prefix = useMemo(() => sharedScopePrefix(scopes), [scopes]);

  if (scopes.length === 0) {
    return <Text small>—</Text>;
  }
  return maxLines ? (
    <ClampedScopes scopes={scopes} prefix={prefix} maxLines={maxLines} />
  ) : (
    <CollapsibleScopes scopes={scopes} prefix={prefix} />
  );
}

function CollapsibleScopes({
  scopes,
  prefix,
}: {
  scopes: string[];
  prefix: string;
}): JSX.Element {
  const { collapsible, expanded, toggle, visible } = useCollapsedPreview(
    scopes,
    PREVIEW_COUNT,
  );
  const listId = useId();

  return (
    <ul id={listId} className="flex flex-wrap items-center gap-1">
      {visible.map((scope) => (
        <li key={scope} className="max-w-full">
          <ScopeChip scope={scope} prefix={prefix} />
        </li>
      ))}
      {collapsible && (
        <li>
          <MoreToggle
            expanded={expanded}
            onToggle={toggle}
            collapsedLabel={`+${scopes.length - PREVIEW_COUNT} more`}
            controlId={listId}
            className="px-1"
          />
        </li>
      )}
    </ul>
  );
}

function ClampedScopes({
  scopes,
  prefix,
  maxLines,
}: {
  scopes: string[];
  prefix: string;
  maxLines: number;
}): JSX.Element {
  const listRef = useRef<HTMLUListElement>(null);
  // Chips wrap freely, so the clip is measured: the list is cut at the top of
  // the first line past maxLines, and every chip from there on is counted.
  const [clip, setClip] = useState<{ height: number; hidden: number } | null>(
    null,
  );

  useLayoutEffect(() => {
    const list = listRef.current;
    if (!list) return;
    const measure = () => {
      const top = list.getBoundingClientRect().top;
      const items = Array.from(list.children).map((item) =>
        item.getBoundingClientRect(),
      );
      const lineTops = [...new Set(items.map((item) => item.top))].sort(
        (a, b) => a - b,
      );
      const cut = lineTops[maxLines];
      if (cut === undefined) {
        setClip(null);
        return;
      }
      const shown = items.filter((item) => item.top < cut);
      setClip({
        height: Math.max(...shown.map((item) => item.bottom)) - top,
        hidden: items.length - shown.length,
      });
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(list);
    return () => observer.disconnect();
  }, [scopes, maxLines]);

  return (
    <div className="flex flex-col gap-1">
      <ul
        ref={listRef}
        className="flex flex-wrap items-center gap-1 overflow-hidden"
        style={clip ? { maxHeight: clip.height } : undefined}
      >
        {scopes.map((scope) => (
          <li key={scope} className="max-w-full">
            <ScopeChip scope={scope} prefix={prefix} />
          </li>
        ))}
      </ul>
      {clip && clip.hidden > 0 && (
        <Text small muted>
          and {clip.hidden} more
        </Text>
      )}
    </div>
  );
}

/**
 * One scope. A scope shown without its shared base lays its full name over
 * the chip on hover.
 */
function ScopeChip({
  scope,
  prefix,
}: {
  scope: string;
  prefix: string;
}): JSX.Element {
  const label = shortScope(scope, prefix);
  const chip = (
    <Badge
      background={false}
      // Scopes are case-sensitive, so the badge must not uppercase them.
      className={`max-w-full ${CHIP_TEXT}`}
    >
      <span className="truncate">{label}</span>
    </Badge>
  );
  if (label === scope) return chip;

  return (
    // No delay: this reveals the rest of a name already on screen.
    <HoverCard openDelay={0}>
      <HoverCardTrigger asChild>{chip}</HoverCardTrigger>
      {/* A chip is 20px tall; pulling the card up by that lands it on the
          chip, with the same border, insets and type. */}
      <HoverCardContent
        align="start"
        side="bottom"
        sideOffset={-20}
        className={`flex h-5 w-auto max-w-none items-center px-1 py-0 whitespace-nowrap duration-75 ${CHIP_TEXT}`}
      >
        {scope}
      </HoverCardContent>
    </HoverCard>
  );
}
