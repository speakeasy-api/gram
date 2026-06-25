import { toast } from "sonner";
import type {
  ChatMessage,
  RiskResult,
  TelemetryLogRecord,
} from "@gram/client/models/components";

function downloadJsonFile(filename: string, data: unknown) {
  const json = JSON.stringify(data, null, 2);
  const blob = new Blob([json], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}

function getTraceExportSlug(chat: { id: string; title?: string | null }) {
  const titleSlug = chat.title
    ?.toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "")
    .slice(0, 40);

  return titleSlug || chat.id.slice(0, 8);
}

export function exportTraceDataAsJson({
  chatId,
  chat,
  messages,
  telemetryLogLimit,
  telemetryLogs,
  riskResults,
}: {
  chatId: string;
  chat: { id: string; title?: string | null };
  messages: ChatMessage[];
  telemetryLogLimit: number;
  telemetryLogs: TelemetryLogRecord[];
  riskResults: RiskResult[];
}): void {
  try {
    const exported = {
      schemaVersion: 1,
      exportScope: "chat_detail_panel",
      exportedAt: new Date().toISOString(),
      chatId,
      telemetryLogsQuery: {
        filter: { gramChatId: chatId },
        limit: telemetryLogLimit,
        loadedCount: telemetryLogs.length,
      },
      panelData: { chat, messages, telemetryLogs, riskResults },
    };
    downloadJsonFile(`trace-${getTraceExportSlug(chat)}.json`, exported);
  } catch {
    toast.error("Failed to export trace data");
  }
}
