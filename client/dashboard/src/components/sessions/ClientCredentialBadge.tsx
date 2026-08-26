import { Badge } from "@/components/ui/Badge";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import {
  credentialKindTooltip,
  CREDENTIAL_KIND_PRESENTATION,
  isBadgedCredentialKind,
  type CredentialKind,
} from "@/lib/user-session-client-credential";

/**
 * What a registered client must present to authenticate, on a listing row.
 *
 * Renders nothing for the two ordinary kinds, and nothing at all when the kind
 * is unknown — a row with no bound client has nothing to say here, and an empty
 * slot says it better than a placeholder. A surface that has to state the kind
 * for every client says it in words instead, as the detail sheet does.
 */
export function ClientCredentialBadge({
  kind,
  declaredMethod,
}: {
  kind: CredentialKind | undefined;
  /** The raw token_endpoint_auth_method, for the tooltip. */
  declaredMethod?: string | null;
}): JSX.Element | null {
  if (!isBadgedCredentialKind(kind)) return null;

  const presentation = CREDENTIAL_KIND_PRESENTATION[kind];
  return (
    <SimpleTooltip tooltip={credentialKindTooltip(kind, declaredMethod)}>
      <Badge
        size="sm"
        variant={presentation.badgeVariant}
        background
        className="shrink-0"
      >
        <Badge.Text>{presentation.label}</Badge.Text>
      </Badge>
    </SimpleTooltip>
  );
}
