import {
  APP_LOADING_NAV_META,
  APP_NAV_GROUPS,
} from "@/components/app-navigation";
import {
  InfoResponseBody,
  OrganizationEntry,
  ProjectEntry,
} from "@gram/client/models/components";
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
} from "@/components/ui/sidebar";
import { createContext, useContext, useEffect } from "react";

import BookDemo from "@/pages/demo/BookDemo";
import { FullPageError } from "@/components/full-page-error";
import { GramLogo } from "@/components/gram-logo";
import { PageHeader } from "@/components/page-header";
import { SessionInfoResponse } from "@gram/client/models/operations";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useSessionInfo } from "@gram/client/react-query";

// We don't include accountType here because it is actively confusing. See useProductTier
type Session = Omit<
  InfoResponseBody,
  "userEmail" | "userId" | "isAdmin" | "gramAccountType"
> & {
  user: User;
  session: string;
  organization: OrganizationEntry;
  rawGramAccountType: string; // "raw" -- should not be used directly unless you know what you are doing
  whitelisted: boolean;
  refetch: () => Promise<Session>;
};

export type User = {
  id: string;
  email: string;
  isAdmin: boolean;
  signature?: string;
  displayName?: string;
  photoUrl?: string;
};

const emptyOrganization: OrganizationEntry = {
  id: "",
  name: "",
  slug: "",
  projects: [],
};

export const emptySession: Session = {
  user: {
    id: "",
    email: "",
    isAdmin: false,
  },
  organizations: [],
  activeOrganizationId: "",
  hasActiveSubscription: false,
  whitelisted: false,
  session: "",
  rawGramAccountType: "",
  organization: emptyOrganization,
  refetch: () => Promise.resolve(emptySession),
};

export const emptyProject = {
  id: "",
  name: "",
  slug: "",
  switchProject: () => {},
};

export const SessionContext = createContext<Session>(emptySession);

export const useSession = () => {
  return useContext(SessionContext);
};

export const ProjectContext = createContext<
  ProjectEntry & {
    switchProject: (slug: string) => void;
  }
>(emptyProject);

export const useProject = () => {
  return useContext(ProjectContext);
};

export const useOrganization = (): OrganizationEntry & {
  refetch: () => Promise<OrganizationEntry>;
} => {
  const { organization, refetch } = useSession();

  const orgRefetch = async () => {
    const newSession = await refetch();
    return (
      newSession.organizations.find((org) => org.id === organization.id) ??
      newSession.organizations[0]!
    );
  };

  return Object.assign(organization, {
    refetch: orgRefetch,
  });
};

export const useSessionData = () => {
  const {
    data: sessionData,
    error,
    refetch,
    status,
  } = useSessionInfo(undefined, undefined, {
    refetchOnWindowFocus: false,
    retry: false,
    throwOnError: false,
  });

  const asSession = (sessionData: SessionInfoResponse): Session => {
    const sessionId = sessionData?.headers["gram-session"]?.[0];
    const result = sessionData.result;

    const organization =
      result.organizations.find(
        (org) => org.id === result.activeOrganizationId,
      ) ?? result.organizations[0];

    return {
      ...result,
      organization: organization ?? emptyOrganization,
      user: {
        id: result.userId,
        email: result.userEmail,
        isAdmin: result.isAdmin,
        signature: result.userSignature,
        displayName: result.userDisplayName,
        photoUrl: result.userPhotoUrl,
      },
      session: sessionId ?? "",
      rawGramAccountType: result.gramAccountType,
      whitelisted: result.whitelisted,
      refetch: async () => {
        const newSession = await refetch();
        return newSession.data ? asSession(newSession.data) : emptySession;
      },
    };
  };

  const session = sessionData ? asSession(sessionData) : null;

  return { session, error, status };
};

/**
 * Lightweight shell that mirrors the real AppLayout structure,
 * shown while the auth session is still loading so the user
 * sees the app chrome immediately instead of a blank screen.
 */
const AppLoadingShell = () => (
  <SidebarProvider
    style={{ "--sidebar-width": "14rem" } as React.CSSProperties}
  >
    <div className="flex h-screen w-full flex-col">
      {/* Header */}
      <header className="dark:bg-background flex h-14 shrink-0 items-center border-b bg-white pr-4 pl-5">
        <div className="flex items-center gap-3">
          <GramLogo className="w-28" />
          <span className="text-muted-foreground/50 text-xl select-none">
            /
          </span>
          <Skeleton className="h-5 w-24" />
          <span className="text-muted-foreground/50 text-xl select-none">
            /
          </span>
          <Skeleton className="h-5 w-20" />
        </div>
        <div className="ml-auto flex items-center gap-4">
          <Skeleton className="h-8 w-8 rounded-full" />
        </div>
      </header>
      {/* Body */}
      <div className="flex w-full flex-1 overflow-hidden pt-2">
        <Sidebar collapsible="offcanvas" variant="inset">
          <SidebarContent className="pt-2">
            {Object.entries(APP_NAV_GROUPS).map(([group, routeKeys]) => (
              <SidebarGroup key={group}>
                <SidebarGroupLabel className="text-sidebar-foreground">
                  {group}
                </SidebarGroupLabel>
                <SidebarGroupContent>
                  <SidebarMenu>
                    {routeKeys.map((routeKey) => {
                      const item = APP_LOADING_NAV_META[routeKey];
                      return (
                        <SidebarMenuItem key={item.label}>
                          <SidebarMenuButton>
                            <Icon
                              name={item.icon}
                              className="text-muted-foreground"
                            />
                            <Type variant="small">{item.label}</Type>
                          </SidebarMenuButton>
                        </SidebarMenuItem>
                      );
                    })}
                  </SidebarMenu>
                </SidebarGroupContent>
              </SidebarGroup>
            ))}
          </SidebarContent>
        </Sidebar>
        <SidebarInset>
          <PageHeader>
            <PageHeader.Breadcrumbs />
            <Loader2 className="text-muted-foreground h-4 w-4 animate-spin" />
          </PageHeader>
        </SidebarInset>
      </div>
    </div>
  </SidebarProvider>
);

export const useUser = () => {
  const { user } = useSession();
  return user;
};

export const useIsAdmin = () => {
  const { isAdmin } = useUser();
  return isAdmin;
};

export function usePylonInAppChat(user: User | undefined) {
  useEffect(() => {
    if (!user) {
      return;
    }
    const random = Math.random().toString(36).substring(7) + "-anonymous";
    const email = user.email;
    const displayName = user.displayName || random;

    window.pylon = {
      chat_settings: {
        app_id: "f9cade16-8d3c-4826-9a2a-034fad495102",
        email: email,
        name: displayName,
        avatar_url: user?.photoUrl,
        ...(user?.signature && { email_hash: user.signature }),
        hide_default_launcher: true,
      },
    };

    if (window.Pylon) {
      window.Pylon("setNewIssueCustomFields", { gram: true });
    }

    // This is for the marketing site
    localStorage.setItem("pylon_user_email", email);
    localStorage.setItem("pylon_user_display_name", displayName);
  }, [user]);
}
