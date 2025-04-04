import { Page } from "@/components/page-layout";
import { Heading } from "@/components/ui/heading";
import { Column, Table } from "@speakeasy-api/moonshine";
import { Key } from "@gram/sdk/models/components"
import { HumanizeDateTime } from "@/lib/dates";
import { Type } from "@/components/ui/type";


const apiKeyColumns: Column<Key>[] = [
  {
    key: "name",
    header: "Name",
    width: "1fr",
    render: (key) => <Type variant="body">{key.name}</Type>,
  },
  {
    key: "createdAt",
    header: "Created At",
    width: "1fr",
    render: (key) => <HumanizeDateTime date={key.createdAt} />,
  },
  {
    key: "updatedAt",
    header: "Updated At",
    width: "1fr",
    render: (key) => <HumanizeDateTime date={key.updatedAt} />,
  },
];

const apiKeys: Key[] = [
  {
    id: "1",
    name: "API Key 1",
    createdAt: new Date(),
    updatedAt: new Date(),
    createdByUserId: "",
    organizationId: "",
    scopes: [],
    token: ""
  },
  {
    id: "2",
    name: "API Key 2",
    createdAt: new Date(),
    updatedAt: new Date(),
    createdByUserId: "",
    organizationId: "",
    scopes: [],
    token: ""
  },
];


export default function Settings() {
  return (
    <Page>
      <Page.Header> 
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Heading variant="h4">API Keys</Heading>
        <Table
          columns={apiKeyColumns}
          data={apiKeys}
          rowKey={(row) => row.id}
        />
      </Page.Body>
    </Page>
  );
}
