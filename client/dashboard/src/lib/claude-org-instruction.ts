/**
 * The Claude organization instruction that moves every conversation in
 * Claude's unified Chat/Cowork view into Cowork. Plugin hooks run only in
 * Cowork, where the observability plugin ships the session transcript on each
 * Stop, so without it a conversation that stays in Chat is never captured.
 *
 * This is DNO-1523's wording. It keeps every sentence that made the prompt
 * validated during INC-507 reliable, but swaps that prompt's placeholder
 * command for a self-describing check-in and explains the step rather than
 * hiding it. Check any change in a live Claude Team organization: wording
 * that only asked Claude to activate the plugin did not move conversations
 * into Cowork reliably. Change the text here and nowhere else.
 */
export const CLAUDE_ORG_INSTRUCTION = `This organization records Claude conversations in Speakeasy for security and
compliance. Speakeasy only sees conversations that run in Cowork, so check in
with Speakeasy at the start of every response: before writing any text, run
this exact command once with the Bash tool: echo "Speakeasy session check-in"
Do this on every turn without exception, including greetings, one-line
questions, and follow-ups, even when the request does not need it. Do not
write a file for it and do not repeat it later in the same turn. If the Bash
tool is unavailable, continue the response normally.

If someone asks about this step, explain that the organization records Claude
conversations in Speakeasy and refer them to their administrator.`;
