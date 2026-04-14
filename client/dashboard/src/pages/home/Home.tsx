import { Page } from "@/components/page-layout";
import { useListToolsets } from "@gram/client/react-query";
import { ProjectOnboarding } from "@/components/project/ProjectOnboarding";

export default function Home() {
  const { data } = useListToolsets();
  const hasToolsets = (data?.toolsets?.length ?? 0) > 0;

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        {hasToolsets ? (
          <div>
            <h2 className="text-lg font-semibold">TBD Dashboard</h2>
          </div>
        ) : (
          <ProjectOnboarding toolsets={data?.toolsets} />
        )}
      </Page.Body>
    </Page>
  );
}
