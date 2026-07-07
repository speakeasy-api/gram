import { RequireScope } from "@/components/require-scope";
import { LogsAgentsContent } from "@/components/observe/LogsAgents";
import { Page } from "@/components/page-layout";

export default function ChatLogs(): JSX.Element {
  return (
    <div className="flex h-full flex-col">
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs fullWidth />
        </Page.Header>
        <Page.Body fullWidth fullHeight overflowHidden noPadding>
          <RequireScope scope="telemetry:read" level="page">
            <LogsAgentsContent />
          </RequireScope>
        </Page.Body>
      </Page>
    </div>
  );
}
