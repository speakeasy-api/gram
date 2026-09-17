import { useCatalog } from "./catalogContext";
import { useMemo, type JSX } from "react";
import { ChevronRight } from "lucide-react";
import { type Draft } from "./model";
import { integrationRequirements } from "./requirements";

export function IntegrationRequirements({
  draft,
  platformIds,
  capabilityIds,
  methodIds,
  scoped,
}: {
  draft: Draft;
  platformIds: string[];
  capabilityIds: string[];
  methodIds: string[];
  scoped: boolean;
}): JSX.Element {
  const { methods, products, capabilities } = useCatalog();
  const result = useMemo(
    () =>
      integrationRequirements(
        draft,
        platformIds.flatMap((platformId) =>
          capabilityIds.map((capabilityId) => ({ platformId, capabilityId })),
        ),
        methods.filter((method) => methodIds.includes(method.id)),
      ),
    [draft, platformIds, capabilityIds, methodIds, methods],
  );
  const allSelected =
    products.every((product) => platformIds.includes(product.id)) &&
    capabilities.every((capability) => capabilityIds.includes(capability.id)) &&
    methods.every((method) => methodIds.includes(method.id));
  const count = platformIds.length * capabilityIds.length;
  let message = "Choose at least one platform and one capability.";
  if (!scoped)
    message =
      "Choose a specific platform or capability in the remaining-dimension filter to calculate product coverage.";
  else if (count && result.gaps.length)
    message =
      "No integration has known full support for the selected platform–capability pairs.";
  return (
    <section
      aria-label="Integration requirements"
      className="bg-muted/40 space-y-3 rounded-lg border p-4"
    >
      <details
        key={allSelected ? "all" : "filtered"}
        open={!allSelected}
        className="group space-y-3"
      >
        <summary className="flex cursor-pointer list-none flex-wrap items-center justify-between gap-2 [&::-webkit-details-marker]:hidden">
          <h2 className="flex items-center gap-2 font-semibold">
            <ChevronRight className="size-4 transition-transform group-open:rotate-90" />
            Integrations required
          </h2>
          <span className="text-muted-foreground text-xs">
            {platformIds.length} platforms · {capabilityIds.length} capabilities
            {result.provisional ? " · Needs verification" : ""}
          </span>
        </summary>
        <div aria-live="polite" aria-atomic="true">
          {scoped && result.combinations.length > 0 ? (
            <div className="space-y-2">
              <p className="text-sm">
                {result.gaps.length
                  ? "To cover the known supported features, you need at least:"
                  : "To cover the selected features, you need at least:"}
              </p>
              {result.combinations.slice(0, 8).map((combination, index) => (
                <div
                  key={combination.join("/")}
                  className="flex flex-wrap items-center gap-2 text-sm"
                >
                  {index > 0 && (
                    <span className="text-muted-foreground font-mono text-xs">
                      OR
                    </span>
                  )}
                  {combination.map((id, methodIndex) => (
                    <span key={id} className="inline-flex items-center gap-2">
                      {methodIndex > 0 && (
                        <span className="text-muted-foreground font-mono text-xs">
                          AND
                        </span>
                      )}
                      <span className="bg-background rounded-md border px-2 py-1 font-medium">
                        {methods.find((method) => method.id === id)!.name}
                      </span>
                    </span>
                  ))}
                </div>
              ))}
              {result.combinations.length > 8 && (
                <details>
                  <summary className="cursor-pointer text-xs">
                    {result.combinations.length - 8} more alternatives
                  </summary>
                  <div className="mt-2 space-y-2">
                    {result.combinations.slice(8).map((combination) => (
                      <p key={combination.join("/")} className="text-sm">
                        OR (
                        {combination
                          .map(
                            (id) =>
                              methods.find((method) => method.id === id)!.name,
                          )
                          .join(" AND ")}
                        )
                      </p>
                    ))}
                  </div>
                </details>
              )}
            </div>
          ) : (
            <p className="text-sm">{message}</p>
          )}
        </div>
        {scoped && result.gaps.length > 0 && (
          <div className="text-sm">
            <p className="font-medium">
              Remaining gaps ({result.gaps.length} of {count})
            </p>
            <p className="text-muted-foreground text-xs">
              {result.unknownCount} with unknown coverage ·{" "}
              {result.partialCount} with partial coverage
            </p>
            <ul className="mt-2 max-h-40 list-disc space-y-1 overflow-auto pl-5">
              {result.gaps.map((target) => (
                <li key={`${target.platformId}/${target.capabilityId}`}>
                  {
                    products.find((product) => product.id === target.platformId)
                      ?.name
                  }{" "}
                  ·{" "}
                  {
                    capabilities.find(
                      (capability) => capability.id === target.capabilityId,
                    )?.name
                  }
                </li>
              ))}
            </ul>
          </div>
        )}
        <p className="text-muted-foreground text-xs">
          Each alternative covers the selected pairs with known full support.
          Partial and unknown coverage remain gaps. Review OS, plan conditions,
          and verification notes before deployment.
        </p>
      </details>
    </section>
  );
}
