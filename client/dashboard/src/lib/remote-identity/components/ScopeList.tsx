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
import {
  type RefObject,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { scopeLabels } from "../model/scopePrefix";

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
  const labels = useMemo(() => scopeLabels(scopes), [scopes]);

  if (scopes.length === 0) {
    return <Text small>—</Text>;
  }
  return maxLines ? (
    <ClampedScopes scopes={scopes} labels={labels} maxLines={maxLines} />
  ) : (
    <CollapsibleScopes scopes={scopes} labels={labels} />
  );
}

function CollapsibleScopes({
  scopes,
  labels,
}: {
  scopes: string[];
  labels: Map<string, string>;
}): JSX.Element {
  const { collapsible, expanded, toggle, visible } = useCollapsedPreview(
    scopes,
    PREVIEW_COUNT,
  );
  const listId = useId();
  const listRef = useRef<HTMLUListElement>(null);
  // Expanding grows the list, which the observer sees.
  const truncated = useTruncatedScopes(listRef, scopes);

  return (
    <ul ref={listRef} id={listId} className="flex flex-wrap items-center gap-1">
      {visible.map((scope) => (
        <li key={scope} className="max-w-full">
          <ScopeChip
            scope={scope}
            label={labels.get(scope)!}
            truncated={truncated.has(scope)}
          />
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
  labels,
  maxLines,
}: {
  scopes: string[];
  labels: Map<string, string>;
  maxLines: number;
}): JSX.Element {
  const listRef = useRef<HTMLUListElement>(null);
  const truncated = useTruncatedScopes(listRef, scopes);
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
      const height = Math.max(...shown.map((item) => item.bottom)) - top;
      const hidden = items.length - shown.length;
      setClip((prev) =>
        prev?.height === height && prev.hidden === hidden
          ? prev
          : { height, hidden },
      );
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
        {scopes.map((scope, i) => (
          <li
            key={scope}
            className="max-w-full"
            // Chips clipped out of view stay out of the tab order and away
            // from screen readers; "and N more" stands in for them.
            inert={!!clip && i >= scopes.length - clip.hidden}
          >
            <ScopeChip
              scope={scope}
              label={labels.get(scope)!}
              truncated={truncated.has(scope)}
            />
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
 * useTruncatedScopes watches a list of chips and returns the scopes whose
 * chip is cut off. Only a scope longer than the whole row truncates, so one
 * observer on the list, not one per chip, catches it.
 */
function useTruncatedScopes(
  listRef: RefObject<HTMLUListElement | null>,
  scopes: string[],
): Set<string> {
  const [truncated, setTruncated] = useState<Set<string>>(() => new Set());
  useLayoutEffect(() => {
    const list = listRef.current;
    if (!list) return;
    const measure = () => {
      const next = new Set<string>();
      for (const text of list.querySelectorAll<HTMLElement>("[data-scope]")) {
        if (text.scrollWidth > text.clientWidth) next.add(text.dataset.scope!);
      }
      setTruncated((prev) =>
        prev.size === next.size && [...next].every((s) => prev.has(s))
          ? prev
          : next,
      );
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(list);
    return () => observer.disconnect();
  }, [listRef, scopes]);
  return truncated;
}

/**
 * One scope. A chip that hides part of its scope, by dropping the shared base
 * or by truncating, takes focus and lays the full scope over itself on hover
 * or focus.
 */
function ScopeChip({
  scope,
  label,
  truncated,
}: {
  scope: string;
  label: string;
  truncated: boolean;
}): JSX.Element {
  const shortened = label !== scope;
  const reveals = shortened || truncated;
  const chip = (
    <Badge
      background={false}
      tabIndex={reveals ? 0 : undefined}
      // Scopes are case-sensitive, so the badge must not uppercase them.
      className={`focus-visible:ring-ring max-w-full focus-visible:ring-1 focus-visible:outline-none ${CHIP_TEXT}`}
    >
      <Badge.Text data-scope={scope} className="truncate">
        {shortened ? (
          <>
            <span aria-hidden>{label}</span>
            <span className="sr-only">{scope}</span>
          </>
        ) : (
          label
        )}
      </Badge.Text>
    </Badge>
  );
  if (!reveals) return chip;

  return (
    // No delay: this reveals the rest of a name already on screen.
    <HoverCard openDelay={0}>
      <HoverCardTrigger asChild>{chip}</HoverCardTrigger>
      {/* A chip is 20px tall; pulling the card up by that lands it on the
          chip, with the same border, insets and type. A scope wider than the
          viewport wraps instead of running off it. */}
      <HoverCardContent
        align="start"
        side="bottom"
        sideOffset={-20}
        className={`flex min-h-5 w-max max-w-[calc(100vw-2rem)] items-center px-1 py-0 break-all duration-75 ${CHIP_TEXT}`}
      >
        {scope}
      </HoverCardContent>
    </HoverCard>
  );
}
