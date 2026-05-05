import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
} from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { useTelemetry } from "@/contexts/Telemetry";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { Navigate, useLocation, useNavigate } from "react-router";
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

export default function Access() {
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

export function AccessInner() {
  const location = useLocation();
  const navigate = useNavigate();
  const { data: rolesData } = useRoles();
  const { data: membersData } = useMembers();
  const roleCount = rolesData?.roles?.length;
  const memberCount = membersData?.members?.length;

  const pathSegments = location.pathname.split("/");
  const lastSegment = pathSegments[pathSegments.length - 1];
  const currentTab = tabFromPath[lastSegment] || "roles";

  const basePath = pathSegments
    .slice(0, lastSegment === "access" ? pathSegments.length : -1)
    .join("/");

  const handleTabChange = (value: string) => {
    navigate(`${basePath}/${value}`);
  };

  return (
    <>
      <div className="-mt-4">
        <Type variant="body" className="text-muted-foreground mb-2">
          Manage access control for your team by defining roles and assigning
          permissions.
        </Type>
      </div>

      <Tabs value={currentTab} onValueChange={handleTabChange}>
        <div className="border-border -mx-8 border-b px-8">
          <TabsList className="h-auto justify-start gap-4 rounded-none bg-transparent p-0 text-sm">
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
        </div>

        <TabsContent value="roles" className="mt-6">
          <RolesTab />
        </TabsContent>

        <TabsContent value="members" className="mt-6">
          <MembersTab />
        </TabsContent>

        <TabsContent value="challenges" className="mt-6">
          <ChallengesTab />
        </TabsContent>
      </Tabs>
    </>
  );
}
