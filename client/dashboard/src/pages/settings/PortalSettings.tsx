import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Type } from "@/components/ui/type";
import { ImageUpload } from "@/components/upload";
import { useProject } from "@/contexts/Auth";
import { PortalPreview } from "@/pages/portal/PortalPreview";
import { usePortal } from "@gram/client/react-query/portal";
import { useUpdatePortalMutation } from "@gram/client/react-query/updatePortal";
import { invalidateAllPortal } from "@gram/client/react-query/portal";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { toast } from "sonner";

export function PortalSettings() {
  const project = useProject();
  const queryClient = useQueryClient();

  const { data: portal } = usePortal(
    { gramProject: project.slug, preview: true },
    undefined,
    { enabled: !!project.slug },
  );

  const update = useUpdatePortalMutation({
    onSuccess: async () => {
      await invalidateAllPortal(queryClient);
      toast.success("Portal settings saved");
    },
    onError: () => {
      toast.error("Failed to save portal settings");
    },
  });

  const [displayName, setDisplayName] = useState("");
  const [tagline, setTagline] = useState("");
  const [enabled, setEnabled] = useState(false);
  // logoAssetId tracks user-driven changes:
  //   undefined = no change since load (preserve existing logo)
  //   ""        = user cleared the logo (send empty string to clear)
  //   <uuid>    = user uploaded a new logo
  const [logoAssetId, setLogoAssetId] = useState<string | undefined>(undefined);
  // logoPreviewUrl mirrors what the form should currently show.
  // Seeded from portal.logoUrl on load; cleared when the user removes the logo.
  const [logoPreviewUrl, setLogoPreviewUrl] = useState<string | undefined>(
    undefined,
  );

  useEffect(() => {
    if (!portal) return;
    setDisplayName(portal.displayName);
    setTagline(portal.tagline ?? "");
    setEnabled(portal.enabled);
    setLogoPreviewUrl(portal.logoUrl || undefined);
    setLogoAssetId(undefined);
  }, [portal]);

  const onSave = () => {
    update.mutate({
      request: {
        updatePortalForm: {
          enabled,
          displayName,
          tagline,
          // Only send logoAssetId if the user actually changed it (upload or clear).
          ...(logoAssetId !== undefined ? { logoAssetId } : {}),
        },
      },
    });
  };

  const portalUrl = `${window.location.origin}/portal/${project.slug}`;

  return (
    <section className="rounded-lg border p-6">
      <Stack gap={4}>
        <div className="flex items-center justify-between">
          <Heading variant="h4">Internal MCP Portal</Heading>
          <Switch
            checked={enabled}
            onCheckedChange={setEnabled}
            aria-label="Enable portal"
          />
        </div>
        <Type muted small>
          Publish a branded catalogue page listing all MCP servers in this
          project. Only org members can access the portal.
        </Type>

        <Stack gap={2}>
          <Label htmlFor="portal-display-name">Display name</Label>
          <Input
            id="portal-display-name"
            value={displayName}
            onChange={setDisplayName}
            placeholder={project.name}
          />
        </Stack>

        <Stack gap={2}>
          <Label>Logo</Label>
          {logoAssetId === undefined && logoPreviewUrl ? (
            // Show the resolved URL from the read API until the user interacts.
            // Clicking removes the logo (mirrors ImageUpload's clear-on-click behaviour).
            <button
              type="button"
              onClick={() => {
                setLogoAssetId("");
                setLogoPreviewUrl(undefined);
              }}
              className="group relative h-16 w-16 cursor-pointer"
              aria-label="Remove logo"
            >
              <img
                src={logoPreviewUrl}
                alt=""
                className="h-16 w-16 rounded object-contain"
              />
              <div className="absolute inset-0 flex items-center justify-center rounded bg-black/50 opacity-0 transition-opacity group-hover:opacity-100">
                <span className="text-xs font-medium text-white">Change</span>
              </div>
            </button>
          ) : (
            <ImageUpload
              onUpload={(asset) => {
                // ImageUpload signals "cleared" with id: ""
                setLogoAssetId(asset.id);
                if (!asset.id) {
                  setLogoPreviewUrl(undefined);
                }
              }}
              existingAssetId={logoAssetId || undefined}
              className="h-16 w-16"
            />
          )}
        </Stack>

        <Stack gap={2}>
          <Label htmlFor="portal-tagline">Tagline</Label>
          <Input
            id="portal-tagline"
            value={tagline}
            onChange={setTagline}
            placeholder="Short tagline shown under the title"
          />
        </Stack>

        <Stack gap={2}>
          <Label>Portal URL</Label>
          <div className="flex items-center gap-2">
            <Input
              readOnly
              value={
                enabled
                  ? portalUrl
                  : "Portal is disabled — enable above to share."
              }
            />
            <Button
              variant="secondary"
              size="md"
              onClick={() => navigator.clipboard.writeText(portalUrl)}
              disabled={!enabled}
            >
              <Button.Text>Copy</Button.Text>
            </Button>
          </div>
        </Stack>

        <Button
          variant="primary"
          size="md"
          onClick={onSave}
          disabled={update.isPending}
        >
          <Button.Text>{update.isPending ? "Saving…" : "Save"}</Button.Text>
        </Button>

        <div className="border-t pt-4">
          <Type variant="subheading" className="mb-2">
            Preview
          </Type>
          <PortalPreview
            projectSlug={project.slug}
            className="h-[600px] w-full rounded-lg border"
          />
        </div>
      </Stack>
    </section>
  );
}
