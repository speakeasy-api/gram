import type { IntegrationDoc } from "./index";
import setupGuide from "../../../../../../../docs/google-chat-assistants.md?raw";

export const GOOGLE_CHAT_DOCS: IntegrationDoc = {
  slug: "googlechat",
  title: "Google Chat",
  summary:
    "Connect a customer-managed Google Chat app: DMs, mentions, native tools, and optional approved thread history.",
  body: `# Setup assistant workflow

Follow this order; Google Chat trigger creation validates its required environment before returning a webhook URL.

1. Discover list_available_tools with urn_prefix tools:platform:googlechat: and list_trigger_definitions. Never invent tool URNs. Ask which capabilities the user wants, explaining that history needs administrator approval.
2. Create the assistant and shared environment. Create a fresh per-assistant toolset with only selected discovered tools; attach_toolset without environment_slug binds the shared assistant environment. Do not attach an unconfigured global provider endpoint instead.
3. Before requesting credentials, briefly explain the Cloud steps that do not need a webhook: enable Google Chat API, configure a non-add-on Chat app, find the numeric project number, and create a service account in the same project with a downloaded JSON key. Leave HTTP endpoint configuration until a webhook is available.
4. Use add_environment_keys for GOOGLE_CHAT_PROJECT_NUMBER and GOOGLE_CHAT_SERVICE_ACCOUNT_JSON. Collect BOTH values using request_environment_secrets, with sensitive:true for GOOGLE_CHAT_SERVICE_ACCOUNT_JSON (the complete JSON). Never ask for credentials in chat or return their values. Wait for the form to finish successfully. Confirm both keys are in saved_keys or list_environments populated_entry_names; merely declaring empty keys is insufficient. If skipped, cancelled, failed, or either value is missing, stop setup and explain what remains. Do not create a trigger, retry its creation, invent credentials, or claim success.
5. Only after BOTH values are populated, call create_trigger with definition_slug googlechat and config matching its discovered schema. Omit environment_id so it binds the same environment as the toolset.
6. Only after trigger creation succeeds, show_google_chat_app_guide with the returned webhook_url. Ask the user to finish Cloud HTTP endpoint configuration with Project Number audience matching GOOGLE_CHAT_PROJECT_NUMBER, then test a DM or @mention before claiming success. The guide is informational, not evidence the user finished setup. Do not configure Slack reply modes or ambient followups.

${setupGuide}`,
};
