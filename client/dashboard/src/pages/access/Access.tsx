import { Page } from "@/components/page-layout";
import { DetailLayout } from "@/components/layouts/detail-layout";
import { RequireScope } from "@/components/require-scope";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
} from "@/components/ui/tabs";
import { useOrganization } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import { useOrgRoutes } from "@/routes";
import { Alert } from "@/components/ui/moonshine";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { Link, Navigate, useLocation, useNavigate } from "react-router";
import { ChallengesTab } from "./ChallengesTab";
import { MembersTab } from "./MembersTab";
import { RolesTab } from "./RolesTab";

const tabFromPath: Record<string, string> = {
  roles: "roles",
  members: "members",
  challenges: "challenges",
};

const tabDisplayNames: Record<string, string> = {
  roles: "Roles & Permissions",
  members: "Roles & Permissions",
  challenges: "Roles & Permissions",
};

export default function Access(): JSX.Element {
  const location = useLocation();
  const telemetry = useTelemetry();
  const isRbacEnabled = telemetry.isFeatureEnabled("gram-rbac") ?? false;

  const pathSegments = location.pathname.split("/");
  const lastSegment = pathSegments[pathSegments.length - 1];
  const shouldRedirect = lastSegment === "access";

  if (!isRbacEnabled) {
    return <Navigate to=".." replace />;
  }

  if (shouldRedirect) {
    return <Navigate to="roles" replace />;
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
  const organization = useOrganization();
  const orgRoutes = useOrgRoutes();
  const { data: rolesData } = useRoles();
  const { data: membersData } = useMembers();
  const roleCount = rolesData?.roles?.length;
  const memberCount = membersData?.members?.length;

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
    <DetailLayout>
      <DetailLayout.Header
        title="Roles & Permissions"
        subtitle="Manage access control for your team by defining roles and assigning permissions. View past authorization challenges."
      />

      {organization.scimEnabled && (
        <Alert variant="info" dismissible={false} className="mt-6 text-sm">
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
        <DetailLayout.Tabs>
          <TabsList className="h-auto justify-start gap-4 bg-transparent p-0 text-sm">
            <PageTabsTrigger value="roles">
              Roles{roleCount != null ? ` (${roleCount})` : ""}
            </PageTabsTrigger>
            <PageTabsTrigger value="members">
              Members{memberCount != null ? ` (${memberCount})` : ""}
            </PageTabsTrigger>
            <PageTabsTrigger value="challenges">
              Authorization Challenges
            </PageTabsTrigger>
          </TabsList>
        </DetailLayout.Tabs>

        <DetailLayout.Content>
          <DetailLayout.Main>
            <TabsContent value="roles">
              <RolesTab />
            </TabsContent>

            <TabsContent value="members">
              <MembersTab />
            </TabsContent>

            <TabsContent value="challenges">
              <ChallengesTab />
            </TabsContent>
          </DetailLayout.Main>
        </DetailLayout.Content>
      </Tabs>
    </DetailLayout>
  );
}
