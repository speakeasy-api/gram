import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import RiskEvents from "./RiskEvents";

export default function RiskEventsPage(): JSX.Element {
  return (
    <div className="flex h-full flex-col">
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs fullWidth />
        </Page.Header>
        <Page.Body fullWidth fullHeight overflowHidden noPadding>
          <RequireScope scope="org:admin" level="page">
            <RiskEvents />
          </RequireScope>
        </Page.Body>
      </Page>
    </div>
  );
}
