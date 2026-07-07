import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { LogsTools } from "@/components/observe/LogsTools";

export function LogsRoot(): JSX.Element {
  return (
    <div className="flex h-full flex-col">
      {/* ^ Wrapper needed to fill page height, allow inner content scrolls. */}
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs fullWidth />
        </Page.Header>
        <Page.Body fullWidth fullHeight overflowHidden noPadding>
          <RequireScope scope="environment:read" level="page">
            <LogsTools />
          </RequireScope>
        </Page.Body>
      </Page>
    </div>
  );
}
