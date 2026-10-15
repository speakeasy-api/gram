import { Link } from "react-router";

import { SettingsSection } from "@/components/detail/settings-section";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";

import { emaServiceHref } from "../tabs";
import { acceptanceLabel } from "./emaServiceView";
import { useEmaServices } from "./useEmaServices";

/**
 * On an identity provider's page: which services accept it. The same facts
 * as the Services table, read from the provider's side.
 */
export function TrustedByServices({
  providerKey,
}: {
  providerKey: string;
}): JSX.Element | null {
  const services = useEmaServices();
  if (services.isPending || services.isError) return null;
  const rows = services.rows.flatMap((row) => {
    const provider = row.providers.find((p) => p.key === providerKey);
    return provider ? [{ row, provider }] : [];
  });
  return (
    <SettingsSection id="trusted-by">
      <SettingsSection.Header>
        <SettingsSection.Title>Trusted by</SettingsSection.Title>
        <SettingsSection.Description>
          Services that accept this identity provider. Each one is set up once,
          in its own admin settings.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {rows.length === 0 ? (
            <Text small muted>
              No service reaches it yet. Turn on Enterprise Managed
              Authorization on an MCP server to start.
            </Text>
          ) : (
            <ul aria-label="Trusted by" className="space-y-2">
              {rows.map(({ row, provider }) => (
                <li key={row.key} className="flex items-baseline gap-2">
                  <span
                    aria-hidden="true"
                    className={cn(
                      "size-2 shrink-0 translate-y-[-1px] rounded-full",
                      provider.lastAcceptedAt
                        ? "bg-[var(--fill-success-default)]"
                        : "bg-muted-foreground/40",
                    )}
                  />
                  <Link
                    to={emaServiceHref(row.vendorIssuer)}
                    className="text-sm font-medium underline underline-offset-4"
                  >
                    {row.name}
                  </Link>
                  <Text muted small>
                    {acceptanceLabel(provider.lastAcceptedAt)}
                  </Text>
                </li>
              ))}
            </ul>
          )}
        </SettingsSection.Body>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
