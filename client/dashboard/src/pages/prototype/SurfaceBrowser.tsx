import { GramLogo } from "@/components/gram-logo";
import { Icon } from "@/components/ui/Icon";
import { cn } from "@/lib/utils";
import { useIdentityTint } from "@/components/gradient-colors";
import {
  findResource,
  RESOURCES,
  type RegisteredResource,
} from "@/resources/registry";
import { useChatBridge } from "@/resources/kit/chat-bridge";
import { useState, type ReactNode } from "react";
import { pageAddress, parseAddress, type PageHistory } from "./page-history";

// ---------------------------------------------------------------------------
// The browser frame
// ---------------------------------------------------------------------------

/**
 * The surface, framed like a browser: a toolbar with back, forward, home and
 * the page's address, then the page itself. Every page the assistant opens
 * lands here, so the frame makes "where am I" answerable at a glance.
 */
export function SurfaceBrowser({
  history,
  project,
  title,
  children,
}: {
  history: PageHistory;
  project: string;
  /** The open page's name, shown before the address. */
  title: string | null;
  children: ReactNode;
}): JSX.Element {
  const address = pageAddress(project, history.page);
  const [draft, setDraft] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const go = () => {
    if (draft == null) return;
    const parsed = parseAddress(draft, project);
    if (parsed === "invalid") {
      setError(`Nothing lives at ${draft.trim()}`);
      return;
    }
    setError(null);
    setDraft(null);
    history.open(parsed);
  };

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex h-12 shrink-0 items-center gap-1 border-b px-2">
        <ToolbarButton
          label="Back"
          icon="arrow-left"
          disabled={!history.canBack}
          onClick={history.back}
        />
        <ToolbarButton
          label="Forward"
          icon="arrow-right"
          disabled={!history.canForward}
          onClick={history.forward}
        />
        <ToolbarButton
          label="Start page"
          icon="house"
          disabled={history.page == null}
          onClick={() => history.open(null)}
        />
        <form
          className="ml-1 min-w-0 flex-1"
          onSubmit={(e) => {
            e.preventDefault();
            go();
          }}
        >
          <label className="bg-surface-secondary-default focus-within:border-neutral-default flex h-8 min-w-0 items-center gap-2 border border-transparent px-2.5 transition-colors">
            <span className="sr-only">Address</span>
            <Icon
              name={history.page ? "panel-right" : "search"}
              className="text-muted-foreground size-3.5 shrink-0"
              aria-hidden="true"
            />
            {title && draft == null && (
              <span className="hidden max-w-[40%] shrink-0 truncate text-sm sm:inline">
                {title}
              </span>
            )}
            <input
              value={draft ?? address}
              onFocus={(e) => {
                setDraft(address);
                e.currentTarget.select();
              }}
              onChange={(e) => setDraft(e.target.value)}
              onBlur={() => {
                setDraft(null);
                setError(null);
              }}
              onKeyDown={(e) => {
                if (e.key === "Escape") e.currentTarget.blur();
              }}
              spellCheck={false}
              className={cn(
                "min-w-0 flex-1 bg-transparent font-mono text-xs outline-none",
                draft == null && "text-muted-foreground",
              )}
            />
          </label>
        </form>
      </div>
      {error && (
        <p
          role="alert"
          className="text-default-destructive border-b px-4 py-2 text-sm"
        >
          {error}
        </p>
      )}
      <div className="min-h-0 flex-1 overflow-y-auto">{children}</div>
    </div>
  );
}

function ToolbarButton({
  label,
  icon,
  disabled,
  onClick,
}: {
  label: string;
  icon: "arrow-left" | "arrow-right" | "house";
  disabled: boolean;
  onClick: () => void;
}): JSX.Element {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      disabled={disabled}
      onClick={onClick}
      className="text-muted-foreground hover:text-foreground hover:bg-surface-secondary-default flex size-8 shrink-0 items-center justify-center transition-colors disabled:pointer-events-none disabled:opacity-40"
    >
      <Icon name={icon} className="size-4" aria-hidden="true" />
    </button>
  );
}

// ---------------------------------------------------------------------------
// Start page
// ---------------------------------------------------------------------------

/**
 * The surface's new-tab page, after Chrome's: the mark, one box that takes an
 * address or a question, shortcut tiles for every page, and a card of what
 * was open recently.
 */
export function StartPage({
  history,
  project,
}: {
  history: PageHistory;
  project: string;
}): JSX.Element {
  const bridge = useChatBridge();
  const [query, setQuery] = useState("");

  const submit = () => {
    const text = query.trim();
    if (!text) return;
    const parsed = parseAddress(text, project);
    if (parsed !== "invalid" && parsed !== null) {
      history.open(parsed);
    } else if (bridge.available) {
      bridge.ask(text);
    }
    setQuery("");
  };

  return (
    <div className="flex min-h-full flex-col items-center px-6 pt-[18vh] pb-16">
      <GramLogo className="w-56" />

      <form
        className="mt-8 w-full max-w-[560px]"
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
      >
        <label className="bg-card focus-within:border-neutral-default hover:border-neutral-default flex h-12 items-center gap-3 border px-4 transition-colors">
          <span className="sr-only">Ask or type an address</span>
          <Icon
            name="search"
            className="text-muted-foreground size-4 shrink-0"
            aria-hidden="true"
          />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Ask Speakeasy or type an address"
            className="placeholder:text-muted-foreground min-w-0 flex-1 bg-transparent text-[15px] outline-none"
          />
          {query.trim() && (
            <button
              type="submit"
              aria-label="Go"
              className="bg-primary text-primary-foreground flex size-7 shrink-0 items-center justify-center"
            >
              <Icon name="arrow-right" className="size-4" aria-hidden="true" />
            </button>
          )}
        </label>
      </form>

      <ul className="mt-10 grid w-full max-w-[560px] grid-cols-1 gap-3 sm:grid-cols-2">
        {RESOURCES.map((r) => (
          <li key={r.id}>
            <ShortcutCard
              resource={r}
              onOpen={() =>
                history.open({ resource: r.id, location: { view: "list" } })
              }
            />
          </li>
        ))}
      </ul>

      {history.recent.length > 0 && (
        <section className="bg-surface-secondary-default mt-8 w-full max-w-[560px] border">
          <h2 className="text-eyebrow px-4 pt-3 pb-2">Recently opened</h2>
          <ul className="bg-card border-t">
            {history.recent.map((page) => {
              const resource = findResource(page.resource);
              return (
                <li
                  key={pageAddress(project, page)}
                  className="border-b last:border-b-0"
                >
                  <button
                    type="button"
                    onClick={() => history.open(page)}
                    className="hover:bg-surface-secondary-default flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors"
                  >
                    <Icon
                      name={resource?.icon ?? "panel-right"}
                      className="text-muted-foreground size-3.5 shrink-0"
                      aria-hidden="true"
                    />
                    <span className="shrink-0 text-sm">
                      {page.location.view === "record"
                        ? (resource?.noun.one ?? "Page")
                        : (resource?.noun.many ?? "Page")}
                    </span>
                    <span className="text-muted-foreground min-w-0 truncate font-mono text-xs">
                      {pageAddress(project, page)}
                    </span>
                  </button>
                </li>
              );
            })}
          </ul>
        </section>
      )}
    </div>
  );
}

/** A page on the start page: what it is, how much is in it, one click away. */
function ShortcutCard({
  resource,
  onOpen,
}: {
  resource: RegisteredResource;
  onOpen: () => void;
}): JSX.Element {
  const count = resource.useCount();
  const tint = useIdentityTint(resource.noun.many);
  return (
    <button
      type="button"
      onClick={onOpen}
      className="bg-card hover:border-neutral-default group flex h-full w-full items-start gap-3 border p-4 text-left transition-colors"
    >
      <span
        aria-hidden="true"
        style={tint}
        className="flex size-10 shrink-0 items-center justify-center"
      >
        <Icon name={resource.icon} className="size-[18px]" />
      </span>
      <span className="flex min-w-0 flex-1 flex-col gap-1">
        <span className="flex items-baseline gap-2">
          <span className="truncate">{resource.noun.many}</span>
          <span className="text-muted-foreground ml-auto shrink-0 font-mono text-xs tabular-nums">
            {count ?? ""}
          </span>
        </span>
        <span className="text-muted-foreground line-clamp-2 text-[13px] leading-snug">
          {resource.description}
        </span>
      </span>
    </button>
  );
}
