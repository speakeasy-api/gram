import { Page } from "@/components/page-layout";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
} from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { useTelemetry } from "@/contexts/Telemetry";
import { useListMembers } from "@gram/client/react-query/listMembers.js";
import { useListRoles } from "@gram/client/react-query/listRoles.js";
import { Navigate, useLocation, useNavigate } from "react-router";
import { MembersTab } from "./MembersTab";
import { RolesTab } from "./RolesTab";

const tabFromPath: Record<string, string> = {
  roles: "roles",
  members: "members",
};

const tabDisplayNames: Record<string, string> = {
  roles: "Roles & Permissions",
  members: "Roles & Permissions",
};

export default function Access() {
  const location = useLocation();
  const navigate = useNavigate();
  const telemetry = useTelemetry();
  const isRbacEnabled = telemetry.isFeatureEnabled("gram-rbac") ?? false;

  const pathSegments = location.pathname.split("/");
  const lastSegment = pathSegments[pathSegments.length - 1];
  const currentTab = tabFromPath[lastSegment] || "roles";
  const shouldRedirect = lastSegment === "access";

  const { data: rolesData } = useListRoles(undefined, undefined, {
    enabled: isRbacEnabled,
  });
  const { data: membersData } = useListMembers(undefined, undefined, {
    enabled: isRbacEnabled,
  });
  const roleCount = rolesData?.roles?.length;
  const memberCount = membersData?.members?.length;

  if (!isRbacEnabled) {
    return <Navigate to=".." replace />;
  }

  const basePath = pathSegments
    .slice(0, lastSegment === "access" ? pathSegments.length : -1)
    .join("/");

  const handleTabChange = (value: string) => {
    navigate(`${basePath}/${value}`);
  };

  if (shouldRedirect) {
    return <Navigate to="roles" replace />;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs substitutions={tabDisplayNames} />
      </Page.Header>
      <Page.Body>
        <div className="-mt-4">
          <Type variant="body" className="text-muted-foreground mb-2">
            Manage access control for your team by defining roles and assigning
            permissions.
          </Type>
        </div>

        <Tabs value={currentTab} onValueChange={handleTabChange}>
          <div className="border-b border-border -mx-8 px-8">
            <TabsList className="bg-transparent p-0 h-auto rounded-none justify-start gap-4 text-sm">
              <PageTabsTrigger value="roles">
                Roles{roleCount != null ? ` (${roleCount})` : ""}
              </PageTabsTrigger>
              <PageTabsTrigger value="members">
                Members{memberCount != null ? ` (${memberCount})` : ""}
              </PageTabsTrigger>
            </TabsList>
          </div>

          <TabsContent value="roles" className="mt-6">
            <RolesTab />
          </TabsContent>

          <TabsContent value="members" className="mt-6">
            <MembersTab />
          </TabsContent>
        </Tabs>
      </Page.Body>
    </Page>
  );
}
