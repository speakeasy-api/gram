import { EnterpriseGate } from "@/components/enterprise-gate";
import { Page } from "@/components/page-layout";

export default function CLIs() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <EnterpriseGate
          icon="terminal"
          title="CLIs"
          description="Build and distribute CLI tools for your API sources. Secure with OAuth and track usage alongside your MCP insights and logs."
        >
          {/* Feature content will go here once CLIs ships */}
          <div />
        </EnterpriseGate>
      </Page.Body>
    </Page>
  );
}
