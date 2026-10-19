import { Link } from "react-router";

import { SettingsSection } from "@/components/detail/settings-section";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { SkeletonParagraph } from "@/components/ui/Skeleton";
import { StatusDot } from "@/components/ui/StatusDot";
import { Text } from "@/components/ui/Text";

import { emaServiceHref } from "../tabs";
import { acceptanceLabel, type ProviderView } from "./emaServiceView";
import { useEmaServices, type ServiceRow } from "./useEmaServices";

type Entry = { row: ServiceRow; provider: ProviderView };

/**
 * On Speakeasy's page: the services that accept Speakeasy, split by
 * whether the service has accepted it yet.
 */
export function TrustedByServices(): JSX.Element {
  const services = useEmaServices();
  const entries: Entry[] = services.rows.flatMap((row) => {
    const provider = row.providers.find((p) => p.kind === "speakeasy");
    return provider ? [{ row, provider }] : [];
  });
  const accepted = entries.filter((entry) => entry.provider.lastAcceptedAt);
  const waiting = entries.filter((entry) => !entry.provider.lastAcceptedAt);

  return (
    <SettingsSection id="services">
      <SettingsSection.Header>
        <SettingsSection.Title>
          Services that trust Speakeasy
        </SettingsSection.Title>
        <SettingsSection.Description>
          Each service must trust the issuer above once, in its own admin
          settings, before Enterprise Managed Authorization works.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {services.isPending ? (
            <>
              <SkeletonParagraph />
              <span role="status" className="sr-only">
                Loading services…
              </span>
            </>
          ) : services.isError ? (
            <Alert variant="error">
              <Text small>Couldn’t load services. Try again.</Text>
              <Button
                variant="secondary"
                size="sm"
                disabled={services.isFetching}
                onClick={services.refetch}
              >
                Retry
              </Button>
            </Alert>
          ) : entries.length === 0 ? (
            <Text small muted>
              None yet. A service shows up here once an MCP server with
              Speakeasy sign-in turns on Enterprise Managed Authorization.
            </Text>
          ) : (
            <div className="space-y-5">
              <EntryList label="Working" entries={accepted} />
              <EntryList label="Hasn’t worked yet" entries={waiting} />
            </div>
          )}
        </SettingsSection.Body>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

function EntryList({
  label,
  entries,
}: {
  label: string;
  entries: Entry[];
}): JSX.Element | null {
  if (entries.length === 0) return null;
  return (
    <div className="space-y-2">
      <Text as="h4" className="text-eyebrow">
        {label}
      </Text>
      <ul aria-label={label} className="space-y-2">
        {entries.map(({ row, provider }) => (
          <li key={row.key} className="flex items-baseline gap-2">
            <StatusDot
              tone={
                !provider.usable
                  ? "warning"
                  : provider.lastAcceptedAt
                    ? "success"
                    : "neutral"
              }
              className="translate-y-[-1px]"
            />
            <Link
              to={emaServiceHref(row.vendorIssuer)}
              className="text-sm font-medium underline underline-offset-4"
            >
              {row.name}
            </Link>
            {(!provider.usable || provider.lastAcceptedAt) && (
              <Text as="span" muted small>
                {provider.usable
                  ? acceptanceLabel(provider.lastAcceptedAt)
                  : provider.unavailable}
              </Text>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}
