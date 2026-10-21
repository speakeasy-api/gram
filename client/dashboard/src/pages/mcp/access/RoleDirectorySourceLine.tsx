import { Text } from "@/components/ui/Text";
import type { RoleDirectorySource } from "@/pages/access/directorySourcesForRole";
import { Link } from "react-router";
import { roleDirectorySourceText } from "./roleDirectorySourceText";

/**
 * Under a role's name on the access list: where the directory hands the role
 * out. A source removed from the directory grants nothing and has a review
 * item waiting on the IDP and SSO tab, so the line links there.
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
  // Wraps rather than truncates: the removed source comes last, and it is
  // the reason the Review link is there.
  return (
    <Text muted small className="min-w-0 break-words">
      {text}
      {hasMissing && (
        <>
          {" · "}
          <Link
            to={reviewHref}
            className="decoration-border hover:decoration-foreground text-foreground underline underline-offset-4"
          >
            Review
          </Link>
        </>
      )}
    </Text>
  );
}
