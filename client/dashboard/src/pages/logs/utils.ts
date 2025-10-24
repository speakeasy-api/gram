import { HTTPToolLog } from "@gram/client/models/components";
import { FileCode, SquareFunction } from "lucide-react";

export interface ParsedUrn {
  kind: string;
  source: string;
  name: string;
}

// Parse URN format: tools:{kind}:{source}:{name}
export const parseUrn = (toolUrn: string): ParsedUrn => {
  const parts = toolUrn.split(":");
  return {
    kind: parts[1] || "",
    source: parts[2] || "",
    name: parts[3] || "",
  };
};

export const getToolIcon = (toolUrn: string) => {
  const { kind } = parseUrn(toolUrn);
  if (kind === "http") {
    return FileCode;
  }
  // Otherwise it's a function tool
  return SquareFunction;
};

export const getSourceFromUrn = (toolUrn: string) => {
  const { source } = parseUrn(toolUrn);
  return source || toolUrn;
};

export const getToolNameFromUrn = (toolUrn: string) => {
  const { name } = parseUrn(toolUrn);
  return name || toolUrn;
};

export const isSuccessfulCall = (log: HTTPToolLog) => {
  // For HTTP tools, check status code
  if (log.httpMethod && log.statusCode) {
    return log.statusCode >= 200 && log.statusCode < 300;
  }
  // For function tools, check success field (when available)
  // For now, default to success for functions
  return true;
};

export const formatTimestamp = (date: Date) => {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  const hours = String(date.getHours()).padStart(2, "0");
  const minutes = String(date.getMinutes()).padStart(2, "0");
  const seconds = String(date.getSeconds()).padStart(2, "0");
  return `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`;
};

export const formatDuration = (ms: number) => {
  if (ms < 1000) {
    return `${ms.toFixed(0)}ms`;
  }
  return `${(ms / 1000).toFixed(1)}s`;
};

export const formatDetailTimestamp = (date: Date) => {
  return date.toLocaleString("en-US", {
    month: "short",
    day: "numeric",
    year: "numeric",
    hour: "numeric",
    minute: "2-digit",
    second: "2-digit",
    timeZoneName: "short",
  });
};

export const getHttpMethodVariant = (method: string): "default" | "secondary" => {
  if (method === "POST") return "default";
  return "secondary";
};