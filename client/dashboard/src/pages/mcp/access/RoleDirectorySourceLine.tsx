import { Text } from "@/components/ui/Text";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import type { RoleDirectorySource } from "@/pages/access/directorySourcesForRole";
import { Link } from "react-router";
import { roleDirectorySourceText } from "./roleDirectorySourceText";

/**
 * Under a role's name on the access list: where the directory hands the role
 * out, on one line, with every source in a tooltip. A source removed from the
 * directory grants nothing and has a review item waiting on the IDP and SSO
 * tab, so the line links there.
 */
export function RoleDirectorySourceLine({
  sources,
  reviewHref,
}: {
  sources: RoleDirectorySource[];
  reviewHref: string;
}): JSX.Element | null {
  const text = roleDirectorySourceText(sources);
  if (!text) return null;
  const hasMissing = sources.some((source) => source.missing);
  // The summary truncates on a narrow row, but the Review link sits outside
  // it so it is never cut off.
  return (
    <div className="flex min-w-0 items-baseline gap-1">
      <SimpleTooltip tooltip={text.full}>
        <Text muted small className="min-w-0 truncate">
          {text.line}
        </Text>
      </SimpleTooltip>
      {hasMissing && (
        <Text muted small className="shrink-0">
          {"· "}
          <Link
            to={reviewHref}
            className="decoration-border hover:decoration-foreground text-foreground underline underline-offset-4"
          >
            Review
          </Link>
        </Text>
      )}
    </div>
  );
}
