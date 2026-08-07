import { Info } from "lucide-react";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { cn } from "@/lib/utils";

// How the billing page explains tokens under management. The copy mirrors the
// public docs section it links to, and every TUM affordance on the page reads
// it from here, so the page can't drift into two competing definitions —
// change the docs section and this copy together.
const TUM_DOCS_URL =
  "https://www.speakeasy.com/docs/ai-control-plane/org-admin/billing#tokens-under-management";

// The tooltip body: the definition, then the two fences that decide what lands
// in the number. It carries its own docs link because pointer users can hover
// into tooltip content (Radix keeps the tooltip open across the gap); keyboard
// users reach the same URL through the trigger, which is itself a link.
export function TumDefinitionHint(): JSX.Element {
  return (
    <div className="flex max-w-xs flex-col gap-1.5">
      <p>
        The volume of agent traffic the platform observes from your users'
        sessions each billing cycle — across supported coding agents — measured
        in tokens.
      </p>
      <p>
        <span className="font-medium">Counted:</span> input tokens, output
        tokens, cache writes (content written to a prompt cache for the first
        time), and MCP tool-call traffic.
      </p>
      <p>
        <span className="font-medium">Not counted:</span> cache reads — that
        content was already counted once, as a cache write — and inference the
        platform runs itself, such as risk-policy analysis, prompt-injection
        scanning, the playground, hosted chat, and the dashboard assistant.
      </p>
      <a
        href={TUM_DOCS_URL}
        target="_blank"
        rel="noreferrer"
        className="underline underline-offset-2"
      >
        Read the billing docs
      </a>
    </div>
  );
}

// The info affordance beside a "Tokens Under Management" label: hover for the
// definition, click (or focus and hit enter) to open the docs section it
// mirrors. An anchor rather than a bare icon so the explanation and the docs
// are both reachable without a pointer.
export function TumDefinitionTooltip({
  className,
}: {
  className?: string;
}): JSX.Element {
  return (
    <SimpleTooltip tooltip={<TumDefinitionHint />}>
      <a
        href={TUM_DOCS_URL}
        target="_blank"
        rel="noreferrer"
        aria-label="How tokens under management is measured"
        className="text-muted-foreground hover:text-foreground inline-flex shrink-0"
      >
        <Info aria-hidden className={cn("h-4 w-4", className)} />
      </a>
    </SimpleTooltip>
  );
}
