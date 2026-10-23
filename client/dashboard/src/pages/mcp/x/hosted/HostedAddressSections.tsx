import { Block, BlockInner } from "@/components/block";
import { FeatureRequestModal } from "@/components/FeatureRequestModal";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import { useTelemetry } from "@/contexts/Telemetry";
import { useProductTier } from "@/hooks/useProductTier";
import { useRBAC } from "@/hooks/useRBAC";
import { useCustomDomain, useMcpUrl } from "@/hooks/useToolsetUrl";
import type { Toolset } from "@/lib/toolTypes";
import { getServerURL } from "@/lib/utils";
import { useMcpSlugValidation } from "@/pages/mcp/mcp-details-utils";
import { PageSection } from "@/pages/mcp/PageSection";
import { useRoutes } from "@/routes";
import { invalidateMcpServerQueries } from "@/lib/mcp-server-visibility";
import { patchMcpEndpointInCache } from "@/hooks/useRootMcpEndpoint";
import {
  invalidateAllMcpEndpoints,
  useMcpEndpoints,
} from "@gram/client/react-query/mcpEndpoints.js";
import { invalidateAllToolset } from "@gram/client/react-query/toolset.js";
import { useUpdateMcpEndpointMutation } from "@gram/client/react-query/updateMcpEndpoint.js";
import { useQueryClient } from "@tanstack/react-query";
import { Globe } from "lucide-react";
import React, { useState } from "react";
import { useParams } from "react-router";
import { toast } from "sonner";

// The server applies canonical endpoint writes to the toolset's hosting columns.
export function HostedAddressSections({
  toolset,
}: {
  toolset: Toolset;
}): React.JSX.Element {
  const { hasScope } = useRBAC();
  const canWrite = hasScope("mcp:write");
  const telemetry = useTelemetry();
  const queryClient = useQueryClient();
  const productTier = useProductTier();
  const { orgSlug } = useParams();
  const { domain } = useCustomDomain();
  const routes = useRoutes();

  const { data: endpointsResult } = useMcpEndpoints({
    mcpServerId: toolset.id,
  });
  const hostedEndpoint = endpointsResult?.mcpEndpoints.find(
    (e) =>
      e.slug === toolset.mcpSlug &&
      (e.customDomainId ?? undefined) === (toolset.customDomainId ?? undefined),
  );

  const updateEndpointMutation = useUpdateMcpEndpointMutation({
    onSuccess: (updated) => {
      patchMcpEndpointInCache(queryClient, updated);
      void invalidateAllToolset(queryClient);
      void invalidateMcpServerQueries(queryClient);
      void invalidateAllMcpEndpoints(queryClient);
      toast.success("MCP settings saved successfully");
      telemetry.capture("mcp_event", {
        action: "mcp_settings_saved",
        slug: toolset.slug,
      });
    },
    onError: () => {
      // Discard staged changes
      setMcpSlug(toolset.mcpSlug || "");
    },
  });

  const [mcpSlug, setMcpSlug] = useState(toolset.mcpSlug || "");
  const [isCustomDomainModalOpen, setIsCustomDomainModalOpen] = useState(false);

  const mcpSlugError = useMcpSlugValidation(
    mcpSlug,
    toolset.mcpSlug,
    toolset.customDomainId,
  );

  const updateHostedEndpoint = (fields: {
    slug: string;
    customDomainId?: string;
  }) => {
    if (!hostedEndpoint) return;
    updateEndpointMutation.mutate({
      request: {
        updateMcpEndpointForm: {
          id: hostedEndpoint.id,
          mcpServerId: toolset.id,
          ...fields,
        },
      },
    });
  };

  const { customServerURL } = useMcpUrl(toolset);

  const handleMcpSlugChange = (value: string) => {
    value = value.slice(0, 40);
    setMcpSlug(value);
  };

  const linkDomainButton = canWrite &&
    domain &&
    domain.activated &&
    domain.verified && (
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant="secondary"
            size="sm"
            className="mr-2"
            disabled={
              !hostedEndpoint ||
              updateEndpointMutation.isPending ||
              !!mcpSlugError ||
              !mcpSlug
            }
            onClick={() => {
              updateHostedEndpoint({
                slug: mcpSlug,
                customDomainId: domain.id,
              });
            }}
          >
            Link Domain
          </Button>
        </TooltipTrigger>
        <TooltipContent>{domain.domain}</TooltipContent>
      </Tooltip>
    );

  // Account for legacy Pro tier which can still access custom domains
  const canAccessCustomDomain = !productTier.includes("base");

  const customDomain =
    domain && canAccessCustomDomain && !toolset.customDomainId ? (
      linkDomainButton
    ) : (
      <RequireScope scope="project:write" level="component">
        <Button
          variant="secondary"
          size="sm"
          onClick={() => {
            if (!canAccessCustomDomain) {
              setIsCustomDomainModalOpen(true);
            } else {
              routes.settings.goTo();
            }
          }}
        >
          Configure
        </Button>
      </RequireScope>
    );

  const anyChanges = mcpSlug !== (toolset.mcpSlug || "");

  const saveButton = anyChanges && (
    <Button
      onClick={() => {
        updateHostedEndpoint({
          slug: mcpSlug,
          customDomainId: toolset.customDomainId,
        });
      }}
      size="sm"
      disabled={!hostedEndpoint || !!mcpSlugError || !mcpSlug || !anyChanges}
    >
      Save
    </Button>
  );

  const discardButton = anyChanges && (
    <Button
      variant="tertiary"
      size="sm"
      onClick={() => {
        setMcpSlug(toolset.mcpSlug || "");
      }}
    >
      Discard
    </Button>
  );

  return (
    <>
      <PageSection
        heading="Custom Slug"
        description="Customize the URL path for your MCP server."
      >
        <Block label="Slug" error={mcpSlugError} className="p-0">
          <BlockInner>
            <Stack direction="horizontal" align="center">
              <Text
                muted
                mono
                variant="small"
                className="hidden @lg/main:block"
              >
                {toolset.mcpSlug && customServerURL
                  ? `${customServerURL}/mcp/`
                  : `${getServerURL()}/mcp/`}
              </Text>
              {!toolset.customDomainId ? (
                <Input
                  className="w-full border px-2 py-1"
                  placeholder="my-server"
                  value={mcpSlug}
                  onChange={handleMcpSlugChange}
                  maxLength={40}
                  requiredPrefix={`${orgSlug}-`}
                  disabled={!canWrite}
                />
              ) : (
                <Input
                  className="w-full border px-2 py-1"
                  placeholder="my-server"
                  value={mcpSlug}
                  onChange={handleMcpSlugChange}
                  maxLength={40}
                  disabled={!toolset.customDomainId || !canWrite}
                />
              )}
              <Stack
                direction="horizontal"
                gap={1}
                align="center"
                className="ml-auto"
              >
                {discardButton}
                {saveButton}
              </Stack>
            </Stack>
          </BlockInner>
        </Block>
      </PageSection>

      <PageSection
        heading="Custom Domain"
        description="Host your MCP server at your own domain."
      >
        <Block label="Domain" className="p-0">
          <BlockInner>
            <Stack direction="horizontal" align="center">
              <Text mono small>
                {toolset.mcpSlug && customServerURL
                  ? `${customServerURL}/mcp/`
                  : `http://mcp.your-company.com/`}
              </Text>
              <Text muted mono small>
                {mcpSlug}
              </Text>
              {!toolset.customDomainId && (
                <div className="ml-auto">{customDomain}</div>
              )}
            </Stack>
          </BlockInner>
        </Block>
      </PageSection>
      <FeatureRequestModal
        isOpen={isCustomDomainModalOpen}
        onClose={() => setIsCustomDomainModalOpen(false)}
        title="Host your MCP at a custom domain"
        description="Custom domains require upgrading to a pro account type. Someone should be in touch shortly, or feel free to book a meeting directly."
        actionType="mcp_custom_domain"
        icon={Globe}
        telemetryData={{ slug: toolset.slug }}
        accountUpgrade
      />
    </>
  );
}
