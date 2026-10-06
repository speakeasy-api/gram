import { SourceSectionError } from "@/components/sources/SourceSectionError";
import { Badge } from "@/components/ui/Badge";
import { Card, Cards } from "@/components/ui/Card";
import { useOrgRoutes } from "@/routes";
import type { WorkloadIssuer } from "@gram/client/models/components/workloadissuer.js";
import { Check } from "lucide-react";
import { Link } from "react-router";
import type { CatalogEntry } from "./definition";
import { connectedIssuer, useCatalogEntries } from "./platforms";
import { isRelativePath } from "./origin";

/** The catalog tab: one card per platform, each opening its own page. */
export function CatalogPlatforms({
  issuers,
  isPending,
}: {
  issuers: WorkloadIssuer[];
  isPending: boolean;
}): JSX.Element {
  const catalog = useCatalogEntries();

  // An empty grid would read as a catalog with nothing in it.
  if (catalog.isError) {
    return (
      <Cards noGrid>
        <SourceSectionError
          heading="Couldn’t load the catalog"
          description="The platforms Gram can connect failed to load. Try again in a moment."
          onRetry={catalog.refetch}
        />
      </Cards>
    );
  }

  return (
    <Cards isLoading={isPending || catalog.isPending} cardSize={2}>
      {catalog.entries.map((entry) => (
        <CatalogCard
          key={entry.key}
          entry={entry}
          connected={connectedIssuer(entry, issuers) !== undefined}
        />
      ))}
    </Cards>
  );
}

function CatalogCard({
  entry,
  connected,
}: {
  entry: CatalogEntry;
  connected: boolean;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const available = entry.enabled && entry.setup !== undefined;
  const card = (
    <Card className="hover:border-foreground/30 h-full transition-colors">
      <Card.Header>
        <div className="flex items-center gap-3">
          {entry.icon !== undefined && isRelativePath(entry.icon) && (
            <img src={entry.icon} alt="" className="h-8 w-8 shrink-0" />
          )}
          <Card.Title>{entry.displayName}</Card.Title>
        </div>
        <Card.Description className="line-clamp-2 !whitespace-normal">
          {entry.description}
        </Card.Description>
      </Card.Header>
      <Card.Content>
        <CatalogStatus available={available} connected={connected} />
      </Card.Content>
    </Card>
  );

  if (!available) {
    return (
      <div aria-disabled className="h-full cursor-not-allowed opacity-50">
        {card}
      </div>
    );
  }
  // A link rather than an onClick, so the card keeps what a link gives for
  // free: middle-click, open in a new tab, and a focus ring for the keyboard.
  return (
    <Link
      to={orgRoutes.workloadIssuers.catalogPlatform.href(entry.key)}
      className="block h-full focus-visible:outline-2 focus-visible:outline-offset-2"
    >
      {card}
    </Link>
  );
}

function CatalogStatus({
  available,
  connected,
}: {
  available: boolean;
  connected: boolean;
}): JSX.Element {
  if (!available) {
    return <Badge variant="neutral">Coming soon</Badge>;
  }
  if (connected) {
    return (
      <Badge variant="success" background>
        <Badge.LeftIcon>
          <Check className="h-3 w-3" />
        </Badge.LeftIcon>
        <Badge.Text>Connected</Badge.Text>
      </Badge>
    );
  }
  return <Badge variant="information">Set up</Badge>;
}
