import { Page } from "@/components/page-layout";
import { Card } from "@/components/ui/card";
import { Toolset } from "@gram/sdk/models/components/toolset";

const data: Toolset[] = [
  {
    id: "1",
    name: "Toolset 1",
    description: "Toolset 1 description",
    organizationId: "1",
    projectId: "1",
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
  },
];

export default function Toolsets() {
  return (
    <Page>
      <Page.Header title="Toolsets" />
      <Page.Body>
        <h1>Toolsets</h1>
        {data.map((toolset) => (
          <Card key={toolset.id}>
            <Card.Header>
              <Card.Title>{toolset.name}</Card.Title>
              <Card.Description>{toolset.description}</Card.Description>
            </Card.Header>
          </Card>
        ))}
      </Page.Body>
    </Page>
  );
}
