import {
  InfoResponseBody,
  OrganizationEntry,
  ProjectEntry,
} from "@gram/client/models/components";
import { SessionInfoResponse } from "@gram/client/models/operations";
import { useSessionInfo } from "@gram/client/react-query";
import { createContext, useContext, useEffect } from "react";
import { useLocation } from "react-router";
import { initializePylon, PYLON_APP_ID } from "@/lib/pylon";
import {
  initializeFermat,
  setFermatProperties,
  trackFermatEvent,
} from "@/lib/fermat";

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

export const useUser = () => {
  const { user } = useSession();
  return user;
};

const SUPER_ADMIN_KEY = "gram-dev-super-admin";

export const useIsAdmin = () => {
  const { isAdmin } = useUser();
  if (import.meta.env.DEV) {
    try {
      const override = localStorage.getItem(SUPER_ADMIN_KEY);
      if (override === "1") return true;
      if (override === "0") return false;
    } catch {
      // ignore
    }
  }
  return isAdmin;
};

export function usePylonInAppChat(user: User | undefined) {
  useEffect(() => {
    if (!user || !import.meta.env.PROD) {
      return;
    }
    const email = user.email;
    const displayName = user.displayName || email;

    const chatSettings = {
      app_id: PYLON_APP_ID,
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

    // This is for the marketing site
    localStorage.setItem("pylon_user_email", email);
    localStorage.setItem("pylon_user_display_name", displayName);
  }, [user]);
}

/**
 * Boot the Claire de Fermat pixel and emit a `page_view` on each route
 * change. The pixel is gated to production builds so we don't pollute
 * Fermat with local/dev traffic (matching Pylon and PostHog).
 */
export function useFermatPixel(user: User | undefined, accountId: string) {
  const location = useLocation();

  useEffect(() => {
    if (!import.meta.env.PROD) {
      return;
    }
    initializeFermat();
    trackFermatEvent("page_view", {
      dashboard_route: location.pathname,
      page_title: document.title,
    });
  }, [location.pathname]);

  // Attach stable identifiers once the user is hydrated so Fermat can
  // attribute activity to the user and their workspace across sessions.
  useEffect(() => {
    if (!import.meta.env.PROD || !user?.id) {
      return;
    }
    initializeFermat();
    setFermatProperties({
      dashboard_user_id: user.id,
      ...(accountId && { account_id: accountId }),
    });
  }, [user?.id, accountId]);
}
