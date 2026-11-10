import { ExternalLinkIcon } from "lucide-react";
import type { JSX } from "react";

import { Button } from "@/components/ui/button";

// An icon button that opens a provider's own page for a record in a new tab.
// `label` is the accessible name and tooltip, so two links in one panel are
// told apart by where they go.
export function ExternalLinkButton({
  href,
  label,
}: {
  href: string;
  label: string;
}): JSX.Element {
  return (
    <Button asChild variant="ghost" size="icon-xs">
      <a
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        aria-label={label}
        title={label}
      >
        <ExternalLinkIcon aria-hidden="true" />
      </a>
    </Button>
  );
}
