import { getServerURL } from "@/lib/utils";
import posthog, { PostHog } from "posthog-js";
import { createContext, ReactNode, useContext, useEffect } from "react";
import { User } from "./Auth";

// Set this to true to test telemetry locally
const AM_TESTING_TELEMETRY = false;

export type Telemetry = Pick<
  PostHog,
  "isFeatureEnabled" | "capture" | "identify" | "register" | "reset" | "group"
>;

const nullTelemetry: Telemetry = {
  isFeatureEnabled: () => false,
  capture: () => ({ uuid: "", event: "", properties: {} }),
  identify: () => {},
  register: () => {},
  reset: () => {},
  group: () => {},
};

const devTelemetry: Telemetry = {
  ...nullTelemetry,
  isFeatureEnabled: () => true,
};

const testTelemetry: Telemetry = {
  capture: (event: string, properties: Record<string, unknown>) => {
    console.log("POSTHOG CAPTURE", event, properties);
    return { uuid: "", event, properties };
  },
  identify: (email: string, properties: Record<string, unknown>) => {
    console.log("POSTHOG IDENTIFY", email, properties);
  },
  register: (properties: Record<string, unknown>) => {
    console.log("POSTHOG REGISTER", properties);
  },
  group: (
    groupType: string,
    groupKey: string,
    properties: Record<string, unknown>
  ) => {
    console.log("POSTHOG GROUP", groupType, groupKey, properties);
  },
  isFeatureEnabled: (feature: string) => {
    console.log("POSTHOG IS_FEATURE_ENABLED", feature);
    return true;
  },
  reset: () => {
    console.log("POSTHOG RESET");
  },
};

export const TelemetryContext = createContext<Telemetry>(
  import.meta.env.DEV ? devTelemetry : nullTelemetry
);

export const useTelemetry = () => useContext(TelemetryContext);

export const TelemetryProvider = (props: { children: ReactNode }) => {
  const ph = posthog.init(
    "phc_hiYSF5Axu49I1xs4Z5BG8KCI3PGNLM8ERRs7eocmfX9",
    {
      api_host: "https://metrics.speakeasy.com",
      feature_flag_request_timeout_ms: 1000,
    },
    "speakeasy"
  );

  let value: Telemetry = ph ?? nullTelemetry;
  if (getServerURL().includes("localhost")) {
    value = AM_TESTING_TELEMETRY ? testTelemetry : devTelemetry;
  }

  return (
    <TelemetryContext.Provider value={value}>
      {props.children}
    </TelemetryContext.Provider>
  );
};

export function useIdentifyUserForTelemetry(user: User | undefined) {
  const telemetry = useTelemetry();

  useEffect(() => {
    // Identify the user
    if (!user?.id) return;
    telemetry.identify(user.email, {
      id: user.id,
      email: user.email,
      admin: user.isAdmin,
      internal: false,
    });
  }, [user, telemetry]);
}

export function useCaptureUserAuthorizationEvent({
  projectSlug,
  organizationSlug,
  email,
}: {
  projectSlug: string;
  organizationSlug: string;
  email: string;
}) {
  const telemetry = useTelemetry();

  useEffect(() => {
    // Capture the event this user authorized for a particular project
    if (!projectSlug) return;
    if (!organizationSlug) return;
    if (!email) return;
    telemetry.capture("authorize_gram_user", {
      email: email,
      project_slug: projectSlug,
      organization_slug: organizationSlug,
      slug: `${organizationSlug}/${projectSlug}`,
    });
  }, [email, projectSlug, organizationSlug, telemetry]);
}

export function useRegisterChatTelemetry({
  chatId,
  chatUrl,
}: {
  chatId: string;
  chatUrl: string;
}) {
  const telemetry = useTelemetry();

  useEffect(() => {
    if (!chatId) return;
    if (!chatUrl) return;

    telemetry.group("chat_id", chatId, {});
    telemetry.register({
      chat_id: chatId,
      chat_url: chatUrl,
    });
  }, [chatId, chatUrl, telemetry]);
}

export function useRegisterEnvironmentTelemetry({
  environmentSlug,
}: {
  environmentSlug: string;
}) {
  const telemetry = useTelemetry();

  useEffect(() => {
    if (!environmentSlug) return;
    telemetry.register({
      environment_slug: environmentSlug,
    });
  }, [environmentSlug, telemetry]);
}

export function useRegisterToolsetTelemetry({
  toolsetSlug,
}: {
  toolsetSlug: string;
}) {
  const telemetry = useTelemetry();

  useEffect(() => {
    if (!toolsetSlug) return;
    telemetry.group("toolset_slug", toolsetSlug, {});
    telemetry.register({
      toolset_slug: toolsetSlug,
    });
  }, [toolsetSlug, telemetry]);
}

export function useRegisterProjectForTelemetry({
  projectSlug,
  organizationSlug,
}: {
  projectSlug: string;
  organizationSlug: string;
}) {
  const telemetry = useTelemetry();

  useEffect(() => {
    if (!projectSlug) return;
    if (!organizationSlug) return;

    // Register the super properties for this workspace to be sent with every event
    telemetry.group("project_slug", projectSlug, {});
    telemetry.group("organization_slug", organizationSlug, {});
    telemetry.group("slug", `${organizationSlug}/${projectSlug}`, {});

    telemetry.register({
      is_gram: true,
      project_slug: projectSlug,
      organization_slug: organizationSlug,
      slug: `${organizationSlug}/${projectSlug}`,
    });
  }, [projectSlug, organizationSlug, telemetry]);
}
