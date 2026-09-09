// Mock data types
export interface IdpProvider {
  id: string;
  name: string;
  /** WorkOS provider_type value passed to intent_options.sso.provider_type */
  providerType: string;
  /** CDN icon slug used with https://cdn.workos.com/provider-icons/{theme}/{slug}.svg */
  iconSlug: string;
  protocol: string;
}

export type PlatformSetupStatus =
  | "not_started"
  | "in_progress"
  | "complete"
  | "blocked";

export interface PlatformSetupStep {
  title: string;
  description?: string;
  code?: string;
  language?: string;
  /** Optional screenshot rendered above the step title, used to point users at
   * a specific UI region (e.g. "the Managed Settings panel in claude.ai").
   * `caption` is rendered as a legend inside the bordered container, below the
   * image — use it to call out what the user should look for or click. */
  screenshot?: { src: string; alt: string; caption?: string };
  /**
   * When true, the instrument-agents component generates a Gram API key with
   * the "hooks" scope on demand and substitutes the literal "{{GRAM_API_KEY}}"
   * marker in `code` with the issued key token.
   */
  requiresApiKey?: boolean;
  /**
   * Optional contextual link rendered below the step description. The
   * `sentence` must contain the literal token "{LINK}", which the renderer
   * replaces with an anchor labeled `linkLabel` pointing at `url`.
   */
  helpLink?: {
    url: string;
    linkLabel: string;
    sentence: string;
  };
  /**
   * When set, the step renders a yes/no eligibility question instead of the
   * standard instructional content. Answering "no" marks the platform as
   * blocked and shows the supplied explanation.
   */
  eligibility?: {
    question: string;
    yesLabel?: string;
    noLabel?: string;
    blockedTitle: string;
    blockedDescription: string;
  };
}

export interface AgentPlatform {
  id: string;
  name: string;
  description: string;
  icon: string;
  connected: boolean;
  /**
   * When false, the platform renders as a grayed-out "coming soon" card and
   * does not contribute to the wizard's configured/total count. Defaults to
   * true when omitted.
   */
  available?: boolean;
  /** Platform-specific setup instructions shown when the card is expanded. */
  setupSteps: PlatformSetupStep[];
}
