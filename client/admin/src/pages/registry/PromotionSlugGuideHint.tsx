import { AlertTriangleIcon, ExternalLinkIcon } from "lucide-react";

import type { JSX } from "react";
import type { SlugGuideHint } from "./registryPromotionGuides";
import { cn } from "@/lib/utils";
import { tone } from "@/lib/tone";

export function PromotionSlugGuideHint({
  id,
  hint,
}: {
  id: string;
  hint: SlugGuideHint | null;
}): JSX.Element {
  return (
    <div id={id} aria-live="polite" className="mt-1 text-xs">
      {hint?.kind === "existing" && (
        <div
          className={cn(
            "flex items-start gap-2 rounded-md border p-2",
            tone.warning,
          )}
        >
          <AlertTriangleIcon
            aria-hidden="true"
            className="mt-0.5 size-4 shrink-0"
          />
          <div className="space-y-1">
            <p>
              <span className="sr-only">Warning: </span>A setup guide for{" "}
              <strong>{hint.title}</strong> already uses this slug. Publishing
              links this server to that guide and won&apos;t request a new one.
              Only keep this slug if it&apos;s the same provider.
            </p>
            <GuideLink url={hint.url} />
          </div>
        </div>
      )}
      {hint?.kind === "endpoint" && (
        <div className="text-muted-foreground space-y-1">
          <p>
            Matches the existing <strong>{hint.title}</strong> guide for this
            endpoint. Publishing links this server to it and won&apos;t request
            a new one.
          </p>
          <GuideLink url={hint.url} />
        </div>
      )}
      {hint?.kind === "none" && (
        <p className="text-muted-foreground">
          No guide in the setup guides released to Speakeasy
          {hint.version ? ` (${hint.version})` : ""} uses this slug, so
          publishing requests a new one. Guides added since then are checked
          when the request is sent.
        </p>
      )}
    </div>
  );
}

function GuideLink({ url }: { url: string }): JSX.Element {
  return (
    <a
      href={url}
      target="_blank"
      rel="noreferrer"
      className="inline-flex items-center gap-1 underline"
    >
      View guide
      <ExternalLinkIcon aria-hidden="true" className="size-3" />
    </a>
  );
}
