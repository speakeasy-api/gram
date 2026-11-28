import { cn } from "@/lib/utils";
import { XIcon } from "lucide-react";
import { useLayoutEffect, useState } from "react";

/**
 * Temporary design-review switcher for the page column width. Open any page
 * with `?pageWidth=1440` (or 1600, 1920, full) to turn it on; the choice
 * sticks in localStorage until you close the switcher. App.css maps each
 * value to --page-max-width and --detail-max-width.
 */

const PAGE_WIDTH_PREVIEW_STORAGE_KEY = "gram:page-width-preview";
const QUERY_PARAM = "pageWidth";

const OPTIONS = [
  { value: "default", label: "1280 (current)" },
  { value: "1440", label: "1440" },
  { value: "1600", label: "1600" },
  { value: "1920", label: "1920" },
  { value: "full", label: "Full" },
] as const;

type PageWidth = (typeof OPTIONS)[number]["value"];

function parsePageWidth(value: string | null): PageWidth | null {
  return OPTIONS.find((o) => o.value === value)?.value ?? null;
}

function readInitial(): PageWidth | null {
  try {
    const fromQuery = parsePageWidth(
      new URLSearchParams(window.location.search).get(QUERY_PARAM),
    );
    if (fromQuery) {
      localStorage.setItem(PAGE_WIDTH_PREVIEW_STORAGE_KEY, fromQuery);
      return fromQuery;
    }
    return parsePageWidth(localStorage.getItem(PAGE_WIDTH_PREVIEW_STORAGE_KEY));
  } catch {
    return null;
  }
}

export function PageWidthPreview(): JSX.Element | null {
  const [width, setWidth] = useState<PageWidth | null>(readInitial);

  useLayoutEffect(() => {
    const root = document.documentElement;
    if (width && width !== "default") root.dataset.pageWidth = width;
    else delete root.dataset.pageWidth;
  }, [width]);

  if (!width) return null;

  const choose = (next: PageWidth | null) => {
    try {
      if (next) localStorage.setItem(PAGE_WIDTH_PREVIEW_STORAGE_KEY, next);
      else localStorage.removeItem(PAGE_WIDTH_PREVIEW_STORAGE_KEY);
    } catch {
      // Storage unavailable: the choice still applies for this page load.
    }
    setWidth(next);
  };

  return (
    <div
      role="group"
      aria-label="Page width preview"
      className="border-border bg-card fixed bottom-4 left-1/2 z-50 flex -translate-x-1/2 items-center gap-1 border p-1 text-xs shadow-lg"
    >
      <span className="text-muted-foreground px-2 font-medium">Page width</span>
      {OPTIONS.map((o) => (
        <button
          key={o.value}
          type="button"
          aria-pressed={width === o.value}
          onClick={() => choose(o.value)}
          className={cn(
            "px-2 py-1",
            width === o.value
              ? "bg-foreground text-background"
              : "hover:bg-muted",
          )}
        >
          {o.label}
        </button>
      ))}
      <button
        type="button"
        aria-label="Close page width preview"
        onClick={() => choose(null)}
        className="hover:bg-muted p-1"
      >
        <XIcon className="size-3.5" />
      </button>
    </div>
  );
}
