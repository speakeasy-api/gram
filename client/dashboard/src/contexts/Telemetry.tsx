import type { PostHog } from "posthog-js";
import { createContext, useContext, useEffect, useReducer } from "react";
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

export const TelemetryContext = createContext<Telemetry>(
  import.meta.env.DEV ? devTelemetry : nullTelemetry,
);

/**
 * Access telemetry, re-rendering the consumer when PostHog feature flags
 * resolve or change.
 *
 * `telemetry.isFeatureEnabled(...)` reads whatever flags PostHog has loaded
 * *so far*. PostHog fetches flags asynchronously after init (and reloads them
 * on `group()`/`identify()`), so a component that reads a flag during render
 * would otherwise be stuck on the pre-load value — most notably opt-in gates
 * (`?? false`) staying hidden forever even once the flag turns on. Subscribing
 * to `onFeatureFlags` here makes every `isFeatureEnabled` call site reactive,
 * so there's a single way to read a flag and it just works.
 */
export const useTelemetry = (): Telemetry => {
  const telemetry = useContext(TelemetryContext);
  const [, onFlagsChanged] = useReducer((version: number) => version + 1, 0);

  useEffect(() => {
    // onFeatureFlags fires once flags are loaded (immediately if already
    // loaded when we subscribe) and again on any reload. Returns an
    // unsubscribe fn. Bumping local state re-renders this consumer so its
    // isFeatureEnabled reads re-evaluate against the latest flags.
    return telemetry.onFeatureFlags(() => onFlagsChanged());
  }, [telemetry]);

  return telemetry;
};

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
    telemetry.group("toolset_slug", toolsetSlug, {});
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

    // Register the super properties for this workspace to be sent with every event
    telemetry.group("project_id", projectId, {});
    telemetry.group("project_slug", projectSlug, {});
    telemetry.group("organization_slug", organizationSlug, {});
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
