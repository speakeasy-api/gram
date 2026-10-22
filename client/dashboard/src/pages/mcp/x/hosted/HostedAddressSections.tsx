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
import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers.js";
import { invalidateAllToolset } from "@gram/client/react-query/toolset.js";
import { useUpdateToolsetMutation } from "@gram/client/react-query/updateToolset.js";
import { useQueryClient } from "@tanstack/react-query";
import { Globe } from "lucide-react";
import React, { useState } from "react";
import { useParams } from "react-router";
import { toast } from "sonner";

/**
 * The canonical hosted server's address. Its endpoint is still owned by the
 * toolset, so the slug and custom domain write through toolsets.update.
 */
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

  const updateToolsetMutation = useUpdateToolsetMutation({
    onSuccess: () => {
      void invalidateAllToolset(queryClient);
      void invalidateAllMcpServers(queryClient);
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

  // Moves to the endpoints table once hosted endpoints are writable through mcpEndpoints.
  const [mcpSlug, setMcpSlug] = useState(toolset.mcpSlug || "");
  const [isCustomDomainModalOpen, setIsCustomDomainModalOpen] = useState(false);

  const mcpSlugError = useMcpSlugValidation(mcpSlug, toolset.mcpSlug);

  const { url: _mcpUrl, customServerURL } = useMcpUrl(toolset);

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
            disabled={updateToolsetMutation.isPending}
            onClick={() => {
              updateToolsetMutation.mutate({
                request: {
                  slug: toolset.slug,
                  updateToolsetRequestBody: {
                    customDomainId: domain.id,
                    mcpSlug: mcpSlug,
                  },
                },
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

  const anyChanges = mcpSlug !== toolset.mcpSlug;

  const saveButton = anyChanges && (
    <Button
      onClick={() => {
        updateToolsetMutation.mutate({
          request: {
            slug: toolset.slug,
            updateToolsetRequestBody: {
              mcpSlug: mcpSlug,
            },
          },
        });
      }}
      size="sm"
      disabled={!!mcpSlugError || !mcpSlug || !anyChanges}
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
