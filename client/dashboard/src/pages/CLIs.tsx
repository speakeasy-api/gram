import { Page } from "@/components/page-layout";
import { ListLayout } from "@/components/layouts/list-layout";
import { RequireScope } from "@/components/require-scope";
import { InlineEmptyState } from "@/components/ui/inline-empty-state";
import { Badge } from "@/components/ui/badge";
import { Terminal } from "lucide-react";

export default function CLIs(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope="project:read" level="page">
          <ListLayout>
            <ListLayout.Header
              title="Skills"
              subtitle="Build and distribute skills with your team. Track usage, enable discovery and improve performance."
            />
            <ListLayout.List>
              <InlineEmptyState
                size="lg"
                icon={<Terminal />}
                title="No skills yet"
                description="Build and distribute skills to your team. Track usage, enable discovery and improve performance."
                action={
                  <Badge variant="neutral" background={false}>
                    <Badge.Text>Coming Soon</Badge.Text>
                  </Badge>
                }
              />
            </ListLayout.List>
          </ListLayout>
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
