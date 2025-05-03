import { InputDialog } from "@/components/input-dialog";
import { Page } from "@/components/page-layout";
import { useSdkClient } from "@/contexts/Sdk";
import {
  useLatestDeployment,
  useListIntegrations,
} from "@gram/client/react-query";
import { useEffect, useState } from "react";
import { useIsAdmin } from "@/contexts/Auth";
import { Button } from "@/components/ui/button";
import { Integration } from "@gram/client/models/components";
import { Card, Cards } from "@/components/ui/card";
import { Badge, Stack } from "@speakeasy-api/moonshine";
import { Type } from "@/components/ui/type";
import { HumanizeDateTime } from "@/lib/dates";
import { AddButton } from "@/components/add-button";
import { CheckIcon } from "lucide-react";

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
      ]}
      onSubmit={handleSubmit}
    />
  );
}

export function IntegrationCard({ integration }: { integration: Integration }) {
  const { data: deployment, refetch } = useLatestDeployment();
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
          <Card.Title>{integration.packageTitle}</Card.Title>
          <div className="flex gap-2 items-center">
            <Badge className="h-6 flex items-center">
              {integration.toolCount || "No"} Tools
            </Badge>
          </div>
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
