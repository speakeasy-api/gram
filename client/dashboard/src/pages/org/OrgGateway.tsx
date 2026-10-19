import { SettingsPage } from "@/components/page-templates";
import {
  GatewayNotServed,
  OrganizationGatewaySwitch,
} from "@/components/mcp/organization-gateway-setting";
import { useOrganizationGatewaySetting } from "@/components/mcp/useOrganizationGatewaySetting";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { useIsCurrentOrganization } from "@/hooks/useIsCurrentOrganization";
import { Network } from "lucide-react";

export default function OrgGateway(): JSX.Element {
  const organization = useOrganization();
  const isCurrentOrganization = useIsCurrentOrganization(organization.id);
  return (
    <SettingsPage
      scope={["org:read", "org:admin"]}
      title="Gateway"
      description="One MCP URL that gives each member every MCP server in the organization they can use, across all projects."
    >
      <GatewaySetting
        key={organization.id}
        organizationId={organization.id}
        isCurrentOrganization={isCurrentOrganization}
      />
    </SettingsPage>
  );
}

function GatewaySetting({
  organizationId,
  isCurrentOrganization,
}: {
  organizationId: string;
  isCurrentOrganization: () => boolean;
}): JSX.Element | null {
  const { features, gateway, on, setOn, isPending } =
    useOrganizationGatewaySetting({ organizationId, isCurrentOrganization });

  if (features.isLoading) return null;

  if (features.error || on === undefined) {
    return (
      <section
        className="border-border bg-card border p-5"
        role="alert"
        aria-live="polite"
      >
        <Text className="text-destructive" small>
          Couldn&apos;t load the gateway setting.
        </Text>
        <Button
          variant="secondary"
          size="sm"
          className="mt-3"
          disabled={features.isFetching}
          onClick={() => void features.refetch()}
        >
          {features.isFetching ? "Retrying…" : "Retry"}
        </Button>
      </section>
    );
  }

  return (
    <section className="border-border bg-card border p-5">
      <div className="flex items-start justify-between gap-3">
        <div className="flex items-start gap-3">
          <div className="border-border flex size-9 shrink-0 items-center justify-center border">
            <Network className="text-muted-foreground size-4" />
          </div>
          <div>
            <Text variant="subheading">Team Gateway</Text>
            <Text small muted className="mt-1 max-w-3xl">
              Members add one URL to their AI client and sign in with their
              organization account. Turning it off stops it working for everyone
              straight away, including clients already connected.
            </Text>
            {on && gateway.data && !gateway.data.available && (
              <GatewayNotServed reason={gateway.data.unavailableReason} />
            )}
            {on && gateway.error && (
              <Text small className="text-warning mt-3" role="alert">
                Couldn&apos;t check whether the gateway is being served.{" "}
                <button
                  type="button"
                  className="underline"
                  disabled={gateway.isFetching}
                  onClick={() => void gateway.refetch()}
                >
                  {gateway.isFetching ? "Checking…" : "Check again"}
                </button>
              </Text>
            )}
          </div>
        </div>
        <OrganizationGatewaySwitch
          on={on}
          setOn={setOn}
          isPending={isPending}
        />
      </div>
    </section>
  );
}
