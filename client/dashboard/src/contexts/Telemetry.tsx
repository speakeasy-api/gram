import type { PostHog } from "posthog-js";
import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useReducer,
} from "react";
import type { ReactNode } from "react";
import type { User } from "./Auth";

export type Telemetry = Pick<
  PostHog,
  | "isFeatureEnabled"
  | "onFeatureFlags"
  | "capture"
  | "identify"
  | "register"
  | "reset"
  | "group"
>;

export const nullTelemetry: Telemetry = {
  isFeatureEnabled: () => false,
  onFeatureFlags: () => () => {},
  capture: () => ({ uuid: "", event: "", properties: {} }),
  identify: () => {},
  register: () => {},
  reset: () => {},
  group: () => {},
};

export const devTelemetry: Telemetry = {
  ...nullTelemetry,
  isFeatureEnabled: () => true,
};

export const testTelemetry: Telemetry = {
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
    properties: Record<string, unknown>,
  ) => {
    console.log("POSTHOG GROUP", groupType, groupKey, properties);
  },
  isFeatureEnabled: (feature: string) => {
    console.log("POSTHOG IS_FEATURE_ENABLED", feature);
    return true;
  },
  onFeatureFlags: () => () => {},
  reset: () => {
    console.log("POSTHOG RESET");
  },
};

type FeatureFlagsStatus = "loading" | "ready" | "error";

type FeatureFlagsSnapshot = {
  status: FeatureFlagsStatus;
  revision: number;
};

export type TelemetryContextValue = {
  telemetry: Telemetry;
  featureFlags: FeatureFlagsSnapshot;
};

const defaultTelemetry = import.meta.env.DEV ? devTelemetry : nullTelemetry;

const TelemetryContext = createContext<TelemetryContextValue>({
  telemetry: defaultTelemetry,
  featureFlags: { status: "ready", revision: 0 },
});

function updateFeatureFlags(
  current: FeatureFlagsSnapshot,
  errorsLoading: boolean,
): FeatureFlagsSnapshot {
  return {
    status: errorsLoading ? "error" : "ready",
    revision: current.revision + 1,
  };
}

/**
 * Owns the single PostHog feature-flag subscription for the dashboard.
 *
 * Updating the context snapshot re-renders existing `useTelemetry` consumers
 * as well as the typed feature-flag hook, without registering one PostHog
 * listener per consumer.
 */
export function TelemetryStateProvider({
  children,
  telemetry,
  featureFlagsInitiallyAvailable = false,
}: {
  children: ReactNode;
  telemetry: Telemetry;
  featureFlagsInitiallyAvailable?: boolean;
}): JSX.Element {
  const [featureFlags, onFlagsChanged] = useReducer(
    updateFeatureFlags,
    featureFlagsInitiallyAvailable
      ? { status: "ready", revision: 0 }
      : { status: "loading", revision: 0 },
  );

  useEffect(() => {
    if (featureFlagsInitiallyAvailable) {
      return undefined;
    }

    return telemetry.onFeatureFlags((_flags, _variants, context) => {
      onFlagsChanged(context?.errorsLoading === true);
    });
  }, [featureFlagsInitiallyAvailable, telemetry]);

  const value = useMemo<TelemetryContextValue>(
    () => ({ telemetry, featureFlags }),
    [featureFlags, telemetry],
  );

  return (
    <TelemetryContext.Provider value={value}>
      {children}
    </TelemetryContext.Provider>
  );
}

export const useTelemetryContext = (): TelemetryContextValue =>
  useContext(TelemetryContext);

/**
 * Access telemetry, re-rendering the consumer when PostHog feature flags
 * resolve or change.
 *
 * `telemetry.isFeatureEnabled(...)` reads whatever flags PostHog has loaded
 * *so far*. PostHog fetches flags asynchronously after init (and reloads them
 * on `group()`/`identify()`), so a component that reads a flag during render
 * would otherwise be stuck on the pre-load value — most notably opt-in gates
 * (`?? false`) staying hidden forever even once the flag turns on. Subscribing
 * at the context provider makes every `isFeatureEnabled` call site reactive,
 * so there is one PostHog listener and a single way to read a flag.
 */
export const useTelemetry = (): Telemetry => useTelemetryContext().telemetry;

export function useIdentifyUserForTelemetry(user: User | undefined): void {
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
  projectId,
  projectSlug,
  organizationSlug,
  email,
}: {
  projectId: string;
  projectSlug: string;
  organizationSlug: string;
  email: string;
}): void {
  const telemetry = useTelemetry();

  useEffect(() => {
    // Capture the event this user authorized for a particular project
    if (!projectId) return;
    if (!projectSlug) return;
    if (!organizationSlug) return;
    if (!email) return;
    telemetry.capture("authorize_gram_user", {
      email: email,
      project_id: projectId,
      project_slug: projectSlug,
      organization_slug: organizationSlug,
      slug: `${organizationSlug}/${projectSlug}`,
    });
  }, [email, projectId, projectSlug, organizationSlug, telemetry]);
}

export function useCaptureEnterpriseGateViewed({
  email,
  organizationId,
  organizationName,
  organizationSlug,
}: {
  email: string;
  organizationId: string;
  organizationName: string;
  organizationSlug: string;
}): void {
  const telemetry = useTelemetry();

  useEffect(() => {
    if (!email) return;
    if (!organizationId) return;
    telemetry.capture("enterprise_gate_viewed", {
      email,
      organization_id: organizationId,
      organization_name: organizationName,
      organization_slug: organizationSlug,
    });
  }, [email, organizationId, organizationName, organizationSlug, telemetry]);
}

// Kept separate from `enterprise_gate_viewed` so that event keeps meaning "cold
// org that never trialed" and the two funnels stay separable.
export function useCaptureTrialExpiredGateViewed({
  email,
  organizationId,
  organizationName,
  organizationSlug,
}: {
  email: string;
  organizationId: string;
  organizationName: string;
  organizationSlug: string;
}): void {
  const telemetry = useTelemetry();

  useEffect(() => {
    if (!email) return;
    if (!organizationId) return;
    telemetry.capture("trial_expired_gate_viewed", {
      email,
      organization_id: organizationId,
      organization_name: organizationName,
      organization_slug: organizationSlug,
    });
  }, [email, organizationId, organizationName, organizationSlug, telemetry]);
}

export function useRegisterChatTelemetry({
  chatId,
  chatUrl,
}: {
  chatId: string;
  chatUrl: string;
}): void {
  const telemetry = useTelemetry();

  useEffect(() => {
    if (!chatId) return;
    if (!chatUrl) return;

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
}): void {
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
}): void {
  const telemetry = useTelemetry();

  useEffect(() => {
    if (!toolsetSlug) return;
    telemetry.register({
      toolset_slug: toolsetSlug,
    });
  }, [toolsetSlug, telemetry]);
}

export function useRegisterProjectForTelemetry({
  projectId,
  projectSlug,
  organizationSlug,
}: {
  projectId: string;
  projectSlug: string;
  organizationSlug: string;
}): void {
  const telemetry = useTelemetry();

  useEffect(() => {
    if (!projectId) return;
    if (!projectSlug) return;
    if (!organizationSlug) return;

    // PostHog caps a project at 5 group types; "organization" and "slug" are the
    // only org/project slots that exist, so we register just those two. Other
    // group types (project_id, project_slug, chat_id, toolset_slug, …) are
    // dropped at ingestion; their values still ship as register() properties.
    telemetry.group("organization", organizationSlug, {});
    telemetry.group("slug", `${organizationSlug}/${projectSlug}`, {});

    telemetry.register({
      is_gram: true,
      project_id: projectId,
      project_slug: projectSlug,
      organization_slug: organizationSlug,
      slug: `${organizationSlug}/${projectSlug}`,
    });
  }, [projectId, projectSlug, organizationSlug, telemetry]);
}
