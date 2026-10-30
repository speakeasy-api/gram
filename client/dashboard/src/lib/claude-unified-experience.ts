// Guidance on Claude's unified Chat and Cowork experience, shared by the setup
// flow and the plugin install dialog so the recommendation never drifts — and
// so lifting it once unified conversations are observable is a change here.

/** Anthropic's guidance on turning the unified experience on or off. */
export const CLAUDE_UNIFIED_EXPERIENCE_DOCS_URL =
  "https://support.claude.com/en/articles/17444462-set-up-and-manage-the-new-unified-claude-experience-for-your-enterprise-organization#h_44c9c4c9a1";

export const CLAUDE_UNIFIED_EXPERIENCE_STEP_TITLE =
  "Keep Chat and Cowork unified turned off";

export const CLAUDE_UNIFIED_EXPERIENCE_NOTE =
  "We recommend keeping Claude's unified Chat and Cowork experience off for now. A unified conversation starts in Chat and moves into Cowork only when a task needs it, so Speakeasy misses the transcript from the Chat portion even when the plugin's hooks fire in the Cowork VM. Plugins distributed only through the device agent are also not registered for that Cowork VM. If Organization settings → Capabilities shows Chat and Cowork unified, an Owner or Primary Owner turns it off, or chooses Keep disabled before it turns on automatically.";
