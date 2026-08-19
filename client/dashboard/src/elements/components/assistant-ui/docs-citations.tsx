import type * as React from "react";
import { BookTextIcon, ExternalLinkIcon } from "lucide-react";
import { cn } from "@/lib/utils";
import { httpsURL, previewText, type DocsExcerpt } from "./search-docs-result";

/**
 * Inline preview cards for the reviewed guides an answer was built from.
 *
 * They read as an unfurl rather than a citation list: the guide is a document
 * the reader may want to open, so the card leads with what it is and who
 * publishes it, the way a shared link does. It renders in the message flow
 * rather than inside the tool card's output disclosure — the source behind a
 * claim is part of the answer, not part of the mechanics of the call.
 */
export function DocsCitations({
  excerpts,
  className,
}: {
  excerpts: DocsExcerpt[];
  className?: string;
}): React.JSX.Element {
  return (
    <div
      data-slot="docs-citations"
      className={cn("flex w-full flex-col gap-2", className)}
    >
      {excerpts.map((excerpt) => (
        <DocsCard
          key={excerpt.uri + (excerpt.heading ?? "")}
          excerpt={excerpt}
        />
      ))}
    </div>
  );
}

function DocsCard({ excerpt }: { excerpt: DocsExcerpt }): React.JSX.Element {
  // The guide's own published page is where a reader should land: it is the
  // same reviewed content, in a form they can open, link, and share. The
  // provider's canonical docs are the fallback for a guide with no page.
  //
  // Both are validated before they reach an anchor. The corpus only ever emits
  // https URLs, but a tool result is assembled by a model-authored compose
  // script, so what arrives here is not guaranteed to be what the corpus sent;
  // a javascript: or data: URL in that position would execute on click.
  const href = httpsURL(excerpt.docs_url) ?? httpsURL(excerpt.links?.[0]);
  const site = href ? hostOf(href) : "Speakeasy AI Control Plane";

  return (
    <article
      data-slot="docs-card"
      className="group flex overflow-hidden border border-border bg-card"
    >
      {/* Unfurls lead with an image. A guide has none, so the thumbnail slot
          carries the document mark — it keeps the card's shape recognisable
          and gives the eye somewhere to land before the title. */}
      <div className="flex w-14 shrink-0 items-center justify-center border-r border-border bg-muted/40">
        <BookTextIcon className="size-5 text-muted-foreground" />
      </div>

      <div className="min-w-0 flex-1 px-4 py-3">
        <div className="flex items-center gap-2 text-[11px] tracking-wide text-muted-foreground uppercase">
          <span className="truncate">{site}</span>
          <span aria-hidden>·</span>
          <span className="truncate">Reviewed setup guide</span>
          {excerpt.stale && (
            <span className="border border-amber-500/40 px-1 py-px text-[10px] font-medium text-amber-600 dark:text-amber-500">
              Needs revalidation
            </span>
          )}
        </div>

        <h4 className="mt-1 truncate text-sm font-semibold text-foreground">
          {href ? (
            <a
              href={href}
              target="_blank"
              rel="noreferrer noopener"
              className="inline-flex max-w-full items-center gap-1 hover:underline"
            >
              <span className="truncate">{excerpt.title}</span>
              <ExternalLinkIcon className="size-3 shrink-0 opacity-0 transition-opacity group-hover:opacity-60" />
            </a>
          ) : (
            excerpt.title
          )}
        </h4>

        {excerpt.heading && (
          <p className="truncate text-xs text-muted-foreground">
            {excerpt.heading}
          </p>
        )}

        <p className="mt-1 line-clamp-2 text-sm text-muted-foreground">
          {previewText(excerpt.excerpt)}
        </p>
      </div>
    </article>
  );
}

/** Canonical links are shown by host: the full URL is long, and what a reader
 * needs in order to judge a source is whose documentation it is. */
function hostOf(link: string): string {
  try {
    return new URL(link).host;
  } catch {
    return link;
  }
}
