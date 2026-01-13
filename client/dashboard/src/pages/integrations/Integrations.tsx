import { AddButton } from "@/components/add-button";
import { AssetImage } from "@/components/asset-image";
import { CreateThingCard } from "@/components/create-thing-card";
import { InputDialog } from "@/components/input-dialog";
import { Page } from "@/components/page-layout";
import { ToolCollectionBadge } from "@/components/tool-collection-badge";
import { Card, Cards } from "@/components/ui/card";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Type } from "@/components/ui/type";
import { useIsAdmin } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { HumanizeDateTime } from "@/lib/dates";
import { IntegrationEntry } from "@gram/client/models/components";
import {
  useLatestDeployment,
  useListIntegrations,
  useListPackagesSuspense,
} from "@gram/client/react-query";
import { Button, Icon, Stack } from "@speakeasy-api/moonshine";
import { CheckIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "@/lib/toast";

export default function Integrations() {
  const { data: integrations, refetch } = useListIntegrations();

  const isAdmin = useIsAdmin();

  const [requestIntegrationDialogOpen, setRequestIntegrationDialogOpen] =
    useState(false);
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
      </Page.Header>
      <Page.Body>
        {isAdmin && (
          <div className="flex justify-end mb-4">
            <AddButton onClick={() => setCreateIntegrationDialogOpen(true)} />
          </div>
        )}
        <Cards>
          {integrations?.integrations?.map((integration) => (
            <IntegrationCard
              key={integration.packageName}
              integration={integration}
              newVersionCallback={() => {
                setCreateIntegrationDialogOpen(true);
              }}
            />
          ))}
          <CreateThingCard
            onClick={() => setRequestIntegrationDialogOpen(true)}
          >
            Request an Integration
          </CreateThingCard>
        </Cards>
        <CreateIntegrationDialog
          open={createIntegrationDialogOpen}
          onOpenChange={setCreateIntegrationDialogOpen}
          onNewVersion={() => {
            refetch();
          }}
        />
        <RequestIntegrationDialog
          open={requestIntegrationDialogOpen}
          onOpenChange={setRequestIntegrationDialogOpen}
        />
      </Page.Body>
    </Page>
  );
}

function CreateIntegrationDialog({
  open,
  onOpenChange,
  onNewVersion,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onNewVersion: () => void;
}) {
  const client = useSdkClient();
  const { data: deployment } = useLatestDeployment();
  const { data: packages, refetch: refetchPackages } =
    useListPackagesSuspense();

  const existingPackage = packages?.packages[0];
  const latestVersion = existingPackage?.latestVersion;

  const [name, setName] = useState(existingPackage?.name ?? "");
  const [summary, setSummary] = useState(existingPackage?.summary ?? "");
  const [keywords, setKeywords] = useState(existingPackage?.keywords ?? []);
  const [imageAssetId, setImageAssetId] = useState(
    existingPackage?.imageAssetId ?? "",
  );
  const [version, setVersion] = useState(latestVersion ?? "");

  const handleSubmit = async () => {
    if (!deployment?.deployment) {
      return;
    }

    const packageName = name.toLowerCase().replace(/ /g, "-");

    if (existingPackage) {
      await client.packages.update({
        updatePackageForm: {
          id: existingPackage.id,
          title: name,
          summary,
          keywords,
          imageAssetId,
        },
      });
    } else {
      await client.packages.create({
        createPackageForm: {
          title: name,
          name: packageName,
          summary,
          keywords,
          imageAssetId,
        },
      });
    }

    await client.packages.publish({
      publishPackageForm: {
        name: packageName,
        version,
        visibility: "public",
        deploymentId: deployment.deployment.id,
      },
    });

    await refetchPackages();
    onNewVersion();

    onOpenChange(false);
  };

  return (
    <InputDialog
      open={open}
      onOpenChange={onOpenChange}
      title={existingPackage ? "Update Integration" : "Create Integration"}
      description="This will turn the contents of the current deployment for this project into an integration."
      inputs={[
        {
          label: "Integration Name",
          value: name,
          onChange: setName,
          placeholder: "Hubspot",
          disabled: !!existingPackage,
        },
        {
          label: "Integration Version",
          value: version,
          onChange: setVersion,
          placeholder: "0.0.1",
          validate: (value) => {
            if (value === latestVersion) {
              return "Version cannot be the same as the latest version";
            }

            if (!value.match(/^\d+\.\d+\.\d+$/)) {
              return "Invalid version format";
            }

            return true;
          },
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
  newVersionCallback,
}: {
  integration: IntegrationEntry;
  newVersionCallback: () => void;
}) {
  const telemetry = useTelemetry();

  const { data: deployment, refetch } = useLatestDeployment();
  const { data: packages } = useListPackagesSuspense();

  const client = useSdkClient();

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

    telemetry.capture("integration_event", {
      action: "integration_enabled",
      integration_name: integration.packageName,
    });
  };

  const handleDisable = async () => {
    await client.deployments.evolveDeployment({
      evolveForm: {
        excludePackages: [integration.packageId],
      },
    });

    telemetry.capture("integration_event", {
      action: "integration_disabled",
      integration_name: integration.packageName,
    });
  };

  const isEnabled = deployment?.deployment?.packages.some(
    (p) => p.name === integration.packageName,
  );

  const toggleEnabled = async () => {
    if (isEnabled) {
      await handleDisable();
    } else {
      await handleEnable();
    }
    refetch();
  };

  const firstParty = packages?.packages.find(
    (p) => p.id === integration.packageId,
  );

  return (
    <Card>
      <Card.Header>
        <Card.Title>
          <Stack direction="horizontal" gap={2} align={"center"}>
            {integration.packageImageAssetId && (
              <AssetImage
                assetId={integration.packageImageAssetId}
                className="w-8 h-8 rounded-md"
              />
            )}
            <span>
              {integration.packageTitle}
              <span className="text-muted-foreground text-sm ml-2">
                v{integration.version}
              </span>
            </span>
          </Stack>
        </Card.Title>
        {firstParty ? (
          <Button variant="secondary" onClick={newVersionCallback}>
            <Button.LeftIcon>
              <Icon name="copy-plus" className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>New Version</Button.Text>
          </Button>
        ) : (
          <Button variant="secondary" onClick={toggleEnabled}>
            {isEnabled ? (
              <>
                <CheckIcon className="w-4 h-4" />
                Enabled
              </>
            ) : (
              "Enable"
            )}
          </Button>
        )}
      </Card.Header>
      <Card.Content>
        <Card.Description>{integration.packageSummary}</Card.Description>
      </Card.Content>
      <Card.Footer>
        <ToolCollectionBadge toolNames={integration.toolNames} />
        <Type variant="body" muted className="text-sm italic">
          {"Updated "}
          <HumanizeDateTime date={new Date(integration.versionCreatedAt)} />
        </Type>
      </Card.Footer>
    </Card>
  );
}

function RequestIntegrationDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const telemetry = useTelemetry();
  const [integrationName, setIntegrationName] = useState("");

  const handleSubmit = () => {
    telemetry.capture("integration_event", {
      action: "integration_requested",
      integration_name: integrationName,
    });
    onOpenChange(false);
    toast.success("Integration requested successfully");
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Request an Integration</Dialog.Title>
        </Dialog.Header>
        <Dialog.Description>
          Not seeing the integration you need? Request it here.
        </Dialog.Description>
        <Stack gap={2}>
          <Heading variant="h5" className="normal-case font-medium">
            What integration are you looking for?
          </Heading>
          <Input
            placeholder="Slack, GitHub, etc."
            value={integrationName}
            onChange={setIntegrationName}
          />
        </Stack>
        <Dialog.Footer>
          <Button variant="tertiary" onClick={() => onOpenChange(false)}>
            Back
          </Button>
          <Button
            onClick={handleSubmit}
            disabled={integrationName.length === 0}
          >
            Request
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
