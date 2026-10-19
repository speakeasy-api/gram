import { Link } from "react-router";
import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { Heading } from "@/components/ui/Heading";
import { Icon } from "@/components/ui/Icon";
import { SkeletonParagraph } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { ISSUER_STATE_BADGE } from "./speakeasyIssuerView";
import { speakeasyProviderHref } from "./tabs";
import { useEnterpriseIssuer } from "./useEnterpriseIssuer";

/** Speakeasy as a built-in identity provider for Enterprise Managed Authorization. */
export function SpeakeasyProviderCard(): JSX.Element {
  const query = useEnterpriseIssuer();
  const issuer = query.data;
  const badge = issuer ? ISSUER_STATE_BADGE[issuer.state] : undefined;
  return (
    <article
      aria-label="Speakeasy identity provider"
      className="flex min-w-0 flex-col gap-4 border p-6"
    >
      <div className="flex flex-wrap items-center gap-3">
        <Icon name="key-round" className="h-5 w-5" />
        <Heading variant="h3">Speakeasy</Heading>
        {badge && <Badge variant={badge.variant}>{badge.label}</Badge>}
      </div>
      <Text muted small>
        Enterprise Managed Authorization lets your team reach supported MCP
        servers with their organization sign-in, without authorizing each
        service individually.
      </Text>
      {query.isLoading && (
        <>
          <SkeletonParagraph />
          <span role="status" className="sr-only">
            Loading issuer…
          </span>
        </>
      )}
      {query.isError && (
        <Alert variant="error">
          <Text small>Couldn't load the issuer. Try again.</Text>
          <Button
            variant="secondary"
            size="sm"
            disabled={query.isFetching}
            onClick={() => void query.refetch()}
          >
            Retry
          </Button>
        </Alert>
      )}
      {issuer?.issuer && <IssuerUri uri={issuer.issuer} />}
      {issuer && (
        <Button
          asChild
          variant={issuer.issuer ? "secondary" : "primary"}
          size="sm"
          className="self-start"
        >
          <Link to={speakeasyProviderHref()}>
            {issuer.issuer ? "Manage Speakeasy" : "Set up Speakeasy"}
          </Link>
        </Button>
      )}
    </article>
  );
}

export function IssuerUri({ uri }: { uri: string }): JSX.Element {
  return (
    <dl className="space-y-1">
      <dt className="text-eyebrow">Issuer URI</dt>
      <dd className="flex min-w-0 items-center gap-2">
        <code className="min-w-0 text-sm [overflow-wrap:anywhere]">{uri}</code>
        <span className="shrink-0">
          <CopyButton text={uri} tooltip="Copy issuer URI" size="xs" />
        </span>
      </dd>
    </dl>
  );
}
