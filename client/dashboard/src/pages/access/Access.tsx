import { Page } from "@/components/page-layout";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
} from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { Navigate, useLocation, useNavigate } from "react-router";
import { MembersTab } from "./MembersTab";
import { RolesTab } from "./RolesTab";

const tabFromPath: Record<string, string> = {
  roles: "roles",
  members: "members",
};

const tabDisplayNames: Record<string, string> = {
  roles: "Roles",
  members: "Members",
};

export default function Access() {
  const location = useLocation();
  const navigate = useNavigate();

  const pathSegments = location.pathname.split("/");
  const lastSegment = pathSegments[pathSegments.length - 1];
  const currentTab = tabFromPath[lastSegment] || "roles";
  const shouldRedirect = lastSegment === "access";

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
          <Type variant="body" className="text-muted-foreground mb-4">
            Manage access control for your team by defining roles and assigning
            permissions.
          </Type>
        </div>

        <Tabs value={currentTab} onValueChange={handleTabChange}>
          <div className="border-b border-border -mt-2">
            <TabsList className="bg-transparent p-0 h-auto rounded-none justify-start gap-4 text-sm">
              <PageTabsTrigger value="roles">Roles</PageTabsTrigger>
              <PageTabsTrigger value="members">Members</PageTabsTrigger>
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
