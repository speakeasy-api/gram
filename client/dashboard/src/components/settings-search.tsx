import { SearchBar } from "@/components/ui/SearchBar";
import { cn } from "@/lib/utils";
import { AppRoute } from "@/routes";
import Fuse, { type FuseResultMatch } from "fuse.js";
import { useMemo, useState } from "react";
import { useNavigate } from "react-router";

export type SearchableSetting = {
  item: AppRoute;
  title: string;
  section: string;
  /** Settings held on the page, so a search for one lands there. */
  terms: string[];
};

/** Renders `text` with the characters Fuse matched picked out. */
function Highlighted({
  text,
  indices,
}: {
  text: string;
  indices: FuseResultMatch["indices"];
}) {
  const parts: React.ReactNode[] = [];
  let cursor = 0;
  for (const [start, end] of indices) {
    if (start > cursor) parts.push(text.slice(cursor, start));
    parts.push(
      <span key={start} className="text-foreground font-medium">
        {text.slice(start, end + 1)}
      </span>,
    );
    cursor = end + 1;
  }
  parts.push(text.slice(cursor));
  return <>{parts}</>;
}

/**
 * Settings search: matches page titles, section names and each page's setting
 * terms, and lists hits in a dropdown under the field — page first, the
 * matched setting beneath — while the nav stays put behind it. Arrow keys move
 * through hits, Enter opens one, Escape clears the query.
 */
export function SettingsSearch({
  settings,
}: {
  settings: SearchableSetting[];
}): JSX.Element {
  const navigate = useNavigate();
  const [query, setQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);

  const fuse = useMemo(
    () =>
      new Fuse(settings, {
        keys: [
          { name: "title", weight: 3 },
          { name: "terms", weight: 2 },
          { name: "section", weight: 1 },
        ],
        includeMatches: true,
        ignoreLocation: true,
        threshold: 0.25,
      }),
    [settings],
  );
  const needle = query.trim();
  const results = needle ? fuse.search(needle, { limit: 8 }) : [];
  const active = Math.min(activeIndex, Math.max(results.length - 1, 0));

  const open = (setting: SearchableSetting) => {
    setQuery("");
    void navigate(setting.item.href());
  };

  const onKeyDown = (event: React.KeyboardEvent) => {
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      const step = event.key === "ArrowDown" ? 1 : -1;
      setActiveIndex(
        (active + step + results.length) % Math.max(results.length, 1),
      );
    } else if (event.key === "Enter") {
      event.preventDefault();
      const hit = results[active];
      if (hit) open(hit.item);
    } else if (event.key === "Escape" && needle) {
      // Clear the query before Escape can close the settings overlay.
      event.stopPropagation();
      setQuery("");
    }
  };

  return (
    <div role="search" className="relative" onKeyDown={onKeyDown}>
      <SearchBar
        value={query}
        onChange={(value) => {
          setQuery(value);
          setActiveIndex(0);
        }}
        placeholder="Search"
      />
      {needle && (
        <div
          role="listbox"
          aria-label="Matching settings"
          className="bg-card border-border absolute inset-x-0 top-full z-20 mt-1 flex flex-col border p-1 shadow-lg"
        >
          {results.map(({ item: setting, matches }, index) => {
            // Name the setting that matched, unless the title already does.
            const titleMatch = matches?.find((m) => m.key === "title");
            const termMatch = matches?.find((m) => m.key === "terms");
            return (
              <button
                key={setting.item.url}
                type="button"
                role="option"
                aria-selected={index === active}
                onMouseEnter={() => setActiveIndex(index)}
                onClick={() => open(setting)}
                className={cn(
                  "flex items-start gap-2.5 px-3 py-2 text-left",
                  index === active && "bg-accent",
                )}
              >
                <setting.item.Icon className="mt-0.5 size-4 shrink-0" />
                <span className="flex min-w-0 flex-col">
                  <span className="truncate text-sm">
                    {titleMatch ? (
                      <Highlighted
                        text={setting.title}
                        indices={titleMatch.indices}
                      />
                    ) : (
                      setting.title
                    )}
                  </span>
                  {!titleMatch && termMatch?.value && (
                    <span className="text-muted-foreground truncate text-xs">
                      <Highlighted
                        text={termMatch.value}
                        indices={termMatch.indices}
                      />
                    </span>
                  )}
                </span>
              </button>
            );
          })}
          {results.length === 0 && (
            <div className="text-muted-foreground px-3 py-2 text-sm">
              No settings match &ldquo;{needle}&rdquo;
            </div>
          )}
        </div>
      )}
    </div>
  );
}
