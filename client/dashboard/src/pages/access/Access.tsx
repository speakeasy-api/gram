import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import {
  PageTabsList,
  PageTabsTrigger,
  Tabs,
  TabsContent,
} from "@/components/ui/Tabs";
import { useOrganization } from "@/contexts/Auth";
import { useOrgRoutes } from "@/routes";
import { Alert } from "@/components/ui/Alert";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import type { AccessRequest } from "@gram/client/models/components/accessrequest.js";
import { useAccessRequests } from "@gram/client/react-query/accessRequests.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import {
  Link,
  Navigate,
  useLocation,
  useNavigate,
  useSearchParams,
} from "react-router";
import { AccessRequestsTab } from "./AccessRequestsTab";
import { useState } from "react";
import { ChallengesTab } from "./ChallengesTab";
import { GrantAccessDialog } from "./GrantAccessDialog";
import { MCPAssignments } from "./MCPAssignments";
import { RolesTab } from "./RolesTab";

// Member management lives on the Team page; this page is roles and the
// challenges they produced.
const tabFromPath: Record<string, string> = {
  roles: "roles",
  assignments: "assignments",
  challenges: "challenges",
  requests: "requests",
};

const tabDisplayNames: Record<string, string> = {
  roles: "Roles & Permissions",
  assignments: "Roles & Permissions",
  challenges: "Roles & Permissions",
  requests: "Roles & Permissions",
};

export default function Access(): JSX.Element {
  const location = useLocation();

  const pathSegments = location.pathname.split("/");
  const lastSegment = pathSegments[pathSegments.length - 1];
  const shouldRedirect = lastSegment === "access";

  if (shouldRedirect) {
    return <Navigate to="roles" replace />;
  }
  // Member management moved to the Team page; links minted before that (audit
  // entries, emails) still point here.
  if (lastSegment === "members") {
    return <Navigate to="../../team" replace />;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={tabDisplayNames}
          skipSegments={["access"]}
        />
      </Page.Header>
      <Page.Body>
        <RequireScope scope={["org:read", "org:admin"]} level="page">
          <AccessInner />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

function AccessInner() {
  const location = useLocation();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();

  // Access request emails deep-link here with ?grant_user=<id>&scope=<scope>
  // to open a pre-filled one-click grant dialog.
  const grantUserId = searchParams.get("grant_user");
  const grantScope = searchParams.get("scope");
  const grantResourceId = searchParams.get("resource_id") || undefined;
  const grantProjectId = searchParams.get("project_id") || undefined;
  const grantRequestId = searchParams.get("request");

  // Stored access requests, while the rollout is on. Emails and Slack DMs
  // add ?request=<id>, so the grant dialog can approve that request for a
  // fixed window.
  const accessRequestsFlag = useFeatureFlag(FEATURE_FLAGS.accessRequests);
  const accessRequestsOn = accessRequestsFlag.status === "enabled";
  const pendingRequests = useAccessRequests({ status: "pending" }, undefined, {
    enabled: accessRequestsOn,
    throwOnError: false,
  });
  const pendingCount = pendingRequests.data?.requests.length;
  // Keep the request the dialog opened on: once it is decided it leaves the
  // pending list, and the dialog still has to show the outcome.
  const foundRequest = grantRequestId
    ? pendingRequests.data?.requests.find((r) => r.id === grantRequestId)
    : undefined;
  const [openedRequest, setOpenedRequest] = useState<AccessRequest>();
  if (foundRequest && openedRequest?.id !== foundRequest.id) {
    setOpenedRequest(foundRequest);
  }
  const grantRequest =
    grantRequestId && openedRequest?.id === grantRequestId
      ? openedRequest
      : undefined;

  const closeGrantDialog = () => {
    const next = new URLSearchParams(searchParams);
    next.delete("grant_user");
    next.delete("scope");
    next.delete("resource_id");
    next.delete("project_id");
    next.delete("request");
    setSearchParams(next, { replace: true });
  };
  const reviewRequest = (request: AccessRequest) => {
    const next = new URLSearchParams(searchParams);
    next.set("grant_user", request.requesterUserId);
    next.set("scope", request.scope);
    if (request.selector.resourceId && request.selector.resourceId !== "*") {
      next.set("resource_id", request.selector.resourceId);
    }
    next.set("request", request.id);
    setSearchParams(next, { replace: true });
  };
  const organization = useOrganization();
  const orgRoutes = useOrgRoutes();
  const { data: rolesData } = useRoles();
  const roleCount = rolesData?.roles?.length;

  const pathSegments = location.pathname.split("/");
  const lastSegment = pathSegments[pathSegments.length - 1] ?? "";
  const currentTab = tabFromPath[lastSegment] || "roles";

  const basePath = pathSegments
    .slice(0, lastSegment === "access" ? pathSegments.length : -1)
    .join("/");

  const handleTabChange = (value: string) => {
    void navigate(`${basePath}/${value}`);
  };

  return (
    <>
      <Page.Section>
        <Page.Section.Title>Roles &amp; Permissions</Page.Section.Title>
        <Page.Section.Description>
          Manage access control for your team by defining roles and assigning
          permissions. View past authorization challenges.
        </Page.Section.Description>
      </Page.Section>

      {organization.scimEnabled && (
        <Alert variant="info" dismissible={false} className="mb-6 text-sm">
          Directory Sync (SCIM) is enabled. Roles are assigned from your
          identity provider, not here.{" "}
          <Link
            to={orgRoutes.identity.href()}
            className="underline underline-offset-2"
          >
            Manage identity settings
          </Link>
        </Alert>
      )}

      <Tabs value={currentTab} onValueChange={handleTabChange}>
        <div className="border-border -mx-8 border-b px-8">
          <PageTabsList>
            <PageTabsTrigger value="roles">
              Roles{roleCount != null ? ` (${roleCount})` : ""}
            </PageTabsTrigger>
            <PageTabsTrigger value="assignments">
              MCP Assignments
            </PageTabsTrigger>
            <PageTabsTrigger value="challenges">
              Authorization Challenges
            </PageTabsTrigger>
            {accessRequestsOn && (
              <PageTabsTrigger value="requests">
                Access Requests{pendingCount ? ` (${pendingCount})` : ""}
              </PageTabsTrigger>
            )}
          </PageTabsList>
        </div>

        <TabsContent value="roles" className="mt-6">
          <RolesTab />
        </TabsContent>

        <TabsContent value="assignments" className="mt-6">
          <MCPAssignments />
        </TabsContent>

        <TabsContent value="challenges" className="mt-6">
          <ChallengesTab />
        </TabsContent>

        {accessRequestsOn && (
          <TabsContent value="requests" className="mt-6">
            <AccessRequestsTab onReview={reviewRequest} />
          </TabsContent>
        )}
      </Tabs>

      {grantUserId && grantScope && (
        <GrantAccessDialog
          userId={grantUserId}
          scope={grantScope}
          resourceId={grantResourceId}
          projectId={grantProjectId}
          request={grantRequest}
          onClose={closeGrantDialog}
        />
      )}
    </>
  );
}
