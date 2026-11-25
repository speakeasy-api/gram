package chat

// OriginClaudeTranscript marks chat messages stored from an uploaded Claude
// transcript, which is how Claude Cowork sessions are captured. The setup
// wizard's completion check (GetSetupTaskCompletionFacts) and its partial
// index (chat_messages_claude_transcript_project_idx) match this exact value
// in SQL, so changing it means changing both.
const OriginClaudeTranscript = "claude-transcript"
