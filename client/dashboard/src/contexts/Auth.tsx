import type { InfoResponseBody } from "@gram/client/models/components/inforesponsebody.js";
import type { OrganizationEntry } from "@gram/client/models/components/organizationentry.js";
import type { ProjectEntry } from "@gram/client/models/components/projectentry.js";
import type { SessionInfoResponse } from "@gram/client/models/operations/sessioninfo.js";
import { useSessionInfo } from "@gram/client/react-query/sessionInfo.js";
import { createContext, useContext, useEffect } from "react";
import { initializePylon, pylonAppId } from "@/lib/pylon";
import { clearLegacyUserStorage } from "@/lib/logout-storage";

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
  organizationOverride: false,
  trial: null,
  hasActiveSubscription: false,
  whitelisted: false,
  session: "",
  rawGramAccountType: "",
  organization: emptyOrganization,
  refetch: () => Promise.resolve(emptySession),
};

export const emptyProject: ProjectEntry & {
  switchProject: (slug: string) => void;
} = {
  id: "",
  name: "",
  slug: "",
  switchProject: (_slug: string): void => {},
};

export const SessionContext = createContext<Session>(emptySession);

export const useSession = (): Session => {
  return useContext(SessionContext);
};

export const ProjectContext = createContext<
  ProjectEntry & {
    switchProject: (slug: string) => void;
  }
>(emptyProject);

export const useProject = (): ProjectEntry & {
  switchProject: (slug: string) => void;
} => {
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

export const useSessionData = (): {
  session: Session | null;
  error: unknown;
  status: ReturnType<typeof useSessionInfo>["status"];
} => {
  const {
    data: sessionData,
    error,
    refetch,
    status,
  } = useSessionInfo(undefined, undefined, {
    refetchOnWindowFocus: true,
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

export const useUser = (): User => {
  const { user } = useSession();
  return user;
};

const PLATFORM_ADMIN_KEY = "gram-dev-platform-admin";

export const useIsPlatformAdmin = (): boolean => {
  const { isAdmin } = useUser();
  if (import.meta.env.DEV) {
    try {
      const override = localStorage.getItem(PLATFORM_ADMIN_KEY);
      if (override === "1") return true;
      if (override === "0") return false;
    } catch {
      // ignore
    }
  }
  return isAdmin;
};

const SPEAKEASY_STAFF_KEY = "gram-dev-speakeasy-staff";

// useIsSpeakeasyStaff gates the unproxied MCP server source on the
// caller's email domain only, unlike useIsPlatformAdmin which also honors a
// DB admin flag. Mirrors access.IsSpeakeasyStaffEmail exactly, including its
// case-sensitive suffix match: a client that accepted more than the server
// would let the unproxied delete cascade remove the wrappers and then be
// refused on the source.
export const useIsSpeakeasyStaff = (): boolean => {
  const { email } = useUser();
  if (import.meta.env.DEV) {
    try {
      const override = localStorage.getItem(SPEAKEASY_STAFF_KEY);
      if (override === "1") return true;
      if (override === "0") return false;
    } catch {
      // ignore
    }
  }
  return (
    email.endsWith("@speakeasy.com") || email.endsWith("@speakeasyapi.dev")
  );
};

export function usePylonInAppChat(user: User | undefined): void {
  useEffect(() => {
    clearLegacyUserStorage();

    const appId = pylonAppId();
    if (!user || !import.meta.env.PROD || !appId) {
      return;
    }
    const email = user.email;
    const displayName = user.displayName || email;

    const chatSettings = {
      app_id: appId,
      email,
      name: displayName,
      avatar_url: user.photoUrl,
      // email_hash is the server-signed HMAC of the email — Pylon uses it
      // to recognize the returning user and replay their thread history.
      // Without it the visitor is treated as anonymous on every refresh.
      ...(user.signature && { email_hash: user.signature }),
      hide_default_launcher: true,
    };

    // Set chat_settings *before* the Pylon script is injected so the
    // widget sees the identified user on first execution. initializePylon
    // is idempotent; re-running it just refreshes chat_settings.
    initializePylon(chatSettings);
    window.Pylon?.("setNewIssueCustomFields", { gram: true });
  }, [user]);
}
