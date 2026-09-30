import type { JSX } from "react";
import { useQuery } from "@tanstack/react-query";
import { registryOktaCandidatesQuery } from "@/lib/gramAdminClient";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { badgeTone } from "@/lib/badgeTone";
import { OKTA_NAMESPACE, currentOinNames } from "./registryOktaNames";

const REASONS: Record<string, string> = {
  domain: "vendor domain matches a remote or website host",
  title: "vendor token matches the entry title",
  label: "a tenant labels the app with the entry title",
};

type Props = {
  entryId: string;
  text: string;
  disabled: boolean;
  onAdd: (name: string) => void;
};

/**
 * Okta application names observed across tenants that plausibly belong to
 * this entry. Staff confirm each one; nothing is written until Save.
 */
export function RegistryOktaCandidates({
  entryId,
  text,
  disabled,
  onAdd,
}: Props): JSX.Element | null {
  const candidates = useQuery(registryOktaCandidatesQuery(entryId));
  if (candidates.isPending) {
    return (
      <p role="status" className="text-muted-foreground text-sm">
        Looking for Okta applications…
      </p>
    );
  }
  if (candidates.error) {
    return (
      <div role="alert" className="text-sm">
        <p>{candidates.error.message}</p>
        <Button
          variant="outline"
          size="sm"
          disabled={candidates.isFetching}
          onClick={() => void candidates.refetch()}
        >
          Retry
        </Button>
      </div>
    );
  }
  const present = new Set(currentOinNames(text));
  const items = candidates.data.candidates.filter(
    (c) => !present.has(c.oinName),
  );
  if (items.length === 0) return null;
  return (
    <section
      aria-labelledby="registry-okta-candidates"
      className="shrink-0 rounded-md border p-3 text-sm"
    >
      <h3 id="registry-okta-candidates" className="font-medium">
        Okta applications that may belong to this server
      </h3>
      <p className="text-muted-foreground mt-1">
        Observed in synced tenants. Adding one puts it in{" "}
        <code>_meta.{OKTA_NAMESPACE}.oinNames</code>; review, then Save.
      </p>
      <ul className="mt-2 space-y-1">
        {items.map((c) => (
          <li
            key={c.oinName}
            className="flex flex-wrap items-center justify-between gap-2"
          >
            <span className="flex flex-wrap items-center gap-2">
              <code>{c.oinName}</code>
              <Badge variant="outline" className={badgeTone.neutral}>
                {c.organizations} org{c.organizations === 1 ? "" : "s"}
              </Badge>
              {c.signOnModes.map((mode) => (
                <Badge
                  key={mode}
                  variant="outline"
                  className={badgeTone.neutral}
                >
                  {mode}
                </Badge>
              ))}
              <span className="text-muted-foreground">
                {REASONS[c.reason] ?? c.reason}
              </span>
            </span>
            {c.mappedBy ? (
              <span className="text-muted-foreground">
                already mapped by <code>{c.mappedBy}</code>
              </span>
            ) : (
              <Button
                variant="outline"
                size="sm"
                disabled={disabled}
                onClick={() => onAdd(c.oinName)}
              >
                Add {c.oinName}
              </Button>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}
