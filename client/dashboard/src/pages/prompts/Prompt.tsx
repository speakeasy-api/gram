import { Page } from "@/components/page-layout";
import { Card } from "@/components/ui/card";
import { PromptTemplate } from "@gram/client/models/components";
import { useTemplate } from "@gram/client/react-query/index.js";
import { Outlet, useParams } from "react-router";

export function PromptRoot() {
  return <Outlet />;
}

export default function PromptPage() {
  const { promptName } = useParams();
  const { data, error } = useTemplate({
    name: promptName,
  });

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        {error ? <p className="text-red-500">{error.message}</p> : null}
        {data ? (
          <PromptDetails template={data.template} />
        ) : (
          <p className="text-red-500">No data returned for prompt template</p>
        )}
      </Page.Body>
    </Page>
  );
}

function PromptDetails({ template }: { template: PromptTemplate }) {
  return (
    <div>
      <p className="font-seminbold text-lg mb-4">{template.description}</p>
      <Card className="max-w-none">
        <Card.Content>
          <pre className="max-w-full overflow-auto">{template.prompt}</pre>
        </Card.Content>
      </Card>
    </div>
  );
}
