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
import { useMembers } from "@gram/client/react-query/members.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import {
  Link,
  Navigate,
  useLocation,
  useNavigate,
  useSearchParams,
} from "react-router";
import { ChallengesTab } from "./ChallengesTab";
import { GrantAccessDialog } from "./GrantAccessDialog";
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

  const pathSegments = location.pathname.split("/");
  const lastSegment = pathSegments[pathSegments.length - 1];
  const shouldRedirect = lastSegment === "access";

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
  const [searchParams, setSearchParams] = useSearchParams();

  // Access request emails deep-link here with ?grant_user=<id>&scope=<scope>
  // to open a pre-filled one-click grant dialog.
  const grantUserId = searchParams.get("grant_user");
  const grantScope = searchParams.get("scope");
  const grantResourceId = searchParams.get("resource_id") || undefined;

  const closeGrantDialog = () => {
    const next = new URLSearchParams(searchParams);
    next.delete("grant_user");
    next.delete("scope");
    next.delete("resource_id");
    setSearchParams(next, { replace: true });
  };
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
            <PageTabsTrigger value="members">
              Members{memberCount != null ? ` (${memberCount})` : ""}
            </PageTabsTrigger>
            <PageTabsTrigger value="challenges">
              Authorization Challenges
            </PageTabsTrigger>
          </PageTabsList>
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

      {grantUserId && grantScope && (
        <GrantAccessDialog
          userId={grantUserId}
          scope={grantScope}
          resourceId={grantResourceId}
          onClose={closeGrantDialog}
        />
      )}
    </>
  );
}
