import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/badge";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
} from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { useNlPoliciesGet } from "@gram/client/react-query/index.js";
import { Loader2 } from "lucide-react";
import { useParams } from "react-router";

import NLPolicyAuditFeedTab from "./NLPolicyAuditFeedTab";
import NLPolicyConfigureTab from "./NLPolicyConfigureTab";
import NLPolicyQuarantinesTab from "./NLPolicyQuarantinesTab";

export default function NLPolicyDetail() {
  return (
    <RequireScope scope="org:admin" level="page">
      <NLPolicyDetailContent />
    </RequireScope>
  );
}

function NLPolicyDetailContent() {
  const { policyId } = useParams<{ policyId: string }>();
  const { data: policy, isLoading } = useNlPoliciesGet(
    { policyId: policyId ?? "" },
    undefined,
    { enabled: !!policyId },
  );

  if (!policyId) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <div className="text-muted-foreground py-20 text-center text-sm">
            Policy not found.
          </div>
        </Page.Body>
      </Page>
    );
  }

  if (isLoading || !policy) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <div className="flex items-center justify-center py-20">
            <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
          </div>
        </Page.Body>
      </Page>
    );
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{ [policy.id]: policy.name, nl: "NL" }}
        />
      </Page.Header>
      <Page.Body>
        <div className="mb-4 flex items-start justify-between gap-4">
          <div>
            <h2 className="text-lg font-semibold">{policy.name}</h2>
            {policy.description ? (
              <Type small muted>
                {policy.description}
              </Type>
            ) : null}
          </div>
          <div className="flex items-center gap-2">
            <Badge variant="outline">v{policy.version}</Badge>
            <Badge
              variant={
                policy.mode === "enforce"
                  ? "destructive"
                  : policy.mode === "audit"
                    ? "warning"
                    : "secondary"
              }
            >
              {policy.mode}
            </Badge>
          </div>
        </div>

        <Tabs defaultValue="configure">
          <div className="border-border -mx-8 border-b px-8">
            <TabsList className="h-auto justify-start gap-4 rounded-none bg-transparent p-0 text-sm">
              <PageTabsTrigger value="configure">Configure</PageTabsTrigger>
              <PageTabsTrigger value="audit">Audit Feed</PageTabsTrigger>
              <PageTabsTrigger value="quarantines">Quarantines</PageTabsTrigger>
            </TabsList>
          </div>

          <TabsContent value="configure" className="mt-6">
            <NLPolicyConfigureTab policy={policy} />
          </TabsContent>
          <TabsContent value="audit" className="mt-6">
            <NLPolicyAuditFeedTab policy={policy} />
          </TabsContent>
          <TabsContent value="quarantines" className="mt-6">
            <NLPolicyQuarantinesTab policy={policy} />
          </TabsContent>
        </Tabs>
      </Page.Body>
    </Page>
  );
}
