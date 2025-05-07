import { AddButton } from "@/components/add-button";
import { AssetImage } from "@/components/asset-image";
import { InputDialog } from "@/components/input-dialog";
import { Page } from "@/components/page-layout";
import { ToolsBadge } from "@/components/tools-badge";
import { Button } from "@/components/ui/button";
import { Card, Cards } from "@/components/ui/card";
import { Type } from "@/components/ui/type";
import { useIsAdmin } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { HumanizeDateTime } from "@/lib/dates";
import { IntegrationEntry } from "@gram/client/models/components";
import {
  useLatestDeployment,
  useListIntegrations,
} from "@gram/client/react-query";
import { Stack } from "@speakeasy-api/moonshine";
import { CheckIcon } from "lucide-react";
import { useEffect, useState } from "react";

export default function Integrations() {
  const { data: integrations, refetch } = useListIntegrations();
  const isAdmin = useIsAdmin();

  const [createIntegrationDialogOpen, setCreateIntegrationDialogOpen] =
    useState(false);

  useEffect(() => {
    if (!createIntegrationDialogOpen) {
      refetch();
    }
  }, [createIntegrationDialogOpen]);

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
        {isAdmin && (
          <Page.Header.Actions>
            <AddButton
              onClick={() => setCreateIntegrationDialogOpen(true)}
              tooltip="New Integration"
            />
          </Page.Header.Actions>
        )}
      </Page.Header>
      <Page.Body>
        <Cards>
          {integrations?.integrations?.map((integration) => (
            <IntegrationCard
              key={integration.packageName}
              integration={integration}
            />
          ))}
        </Cards>
        <CreateIntegrationDialog
          open={createIntegrationDialogOpen}
          onOpenChange={setCreateIntegrationDialogOpen}
        />
      </Page.Body>
    </Page>
  );
}

function CreateIntegrationDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const client = useSdkClient();
  const { data: deployment } = useLatestDeployment();

  const [name, setName] = useState("");
  const [summary, setSummary] = useState("");
  const [keywords, setKeywords] = useState<string[]>([]);
  const [imageAssetId, setImageAssetId] = useState<string>();

  const handleSubmit = async () => {
    if (!deployment?.deployment) {
      return;
    }

    const packageName = name.toLowerCase().replace(/ /g, "-");

    await client.packages.create({
      createPackageForm: {
        title: name,
        name: packageName,
        summary,
        keywords,
        imageAssetId,
      },
    });

    await client.packages.publish({
      publishPackageForm: {
        name: packageName,
        version: "0.0.1",
        visibility: "public",
        deploymentId: deployment.deployment.id,
      },
    });

    onOpenChange(false);
  };

  return (
    <InputDialog
      open={open}
      onOpenChange={onOpenChange}
      title="Create Integration"
      description="This will turn the contents of the current deployment for this project into an integration."
      inputs={[
        {
          label: "Integration Name",
          value: name,
          onChange: setName,
          placeholder: "Hubspot",
        },
        {
          label: "Integration Summary",
          value: summary,
          onChange: setSummary,
          placeholder: "Access your Hubspot data in Gram.",
        },
        {
          label: "Integration Keywords",
          value: keywords.join(", "),
          onChange: (value) => setKeywords(value.split(", ")),
          placeholder: "hubspot, crm",
        },
        {
          label: "Integration Image",
          type: "image",
          value: imageAssetId ?? "",
          onChange: setImageAssetId,
        },
      ]}
      onSubmit={handleSubmit}
    />
  );
}

export function IntegrationCard({
  integration,
}: {
  integration: IntegrationEntry;
}) {
  const { data: deployment, refetch } = useLatestDeployment();
  const client = useSdkClient();

  console.log(deployment?.deployment, integration);

  const handleEnable = async () => {
    await client.deployments.evolveDeployment({
      evolveForm: {
        upsertPackages: [
          {
            name: integration.packageName,
            version: integration.version,
          },
        ],
      },
    });
  };

  const handleDisable = async () => {
    await client.deployments.evolveDeployment({
      evolveForm: {
        excludePackages: [integration.packageId],
      },
    });
  };

  const isEnabled = deployment?.deployment?.packages.some(
    (p) => p.name === integration.packageName
  );

  const toggleEnabled = async () => {
    if (isEnabled) {
      await handleDisable();
    } else {
      await handleEnable();
    }
    refetch();
  };

  return (
    <Card>
      <Card.Header>
        <Stack direction="horizontal" gap={2} justify={"space-between"}>
          <Stack direction="horizontal" gap={2} align={"center"}>
            {integration.packageImageAssetId && (
              <AssetImage
                assetId={integration.packageImageAssetId}
                className="w-8 h-8 rounded-md"
              />
            )}
            <Card.Title>{integration.packageTitle}</Card.Title>
          </Stack>
          <ToolsBadge tools={integration.toolNames} />
        </Stack>
        <Stack direction="horizontal" gap={3} justify={"space-between"}>
          <Card.Description className="max-w-2/3">
            {integration.packageSummary}
          </Card.Description>
          <Type variant="body" muted className="text-sm italic">
            {"Updated "}
            <HumanizeDateTime date={new Date(integration.versionCreatedAt)} />
          </Type>
        </Stack>
      </Card.Header>
      <Card.Footer>
        <Button variant="outline" onClick={toggleEnabled}>
          {isEnabled ? (
            <>
              <CheckIcon className="w-4 h-4" />
              Enabled
            </>
          ) : (
            "Enable"
          )}
        </Button>
      </Card.Footer>
    </Card>
  );
}
