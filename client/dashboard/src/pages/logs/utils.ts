import { HTTPToolLog } from "@gram/client/models/components";
import { FileCode, PencilRuler, SquareFunction } from "lucide-react";
import { dateTimeFormatters } from "@/lib/dates";

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
  if (kind === "prompt") {
    return PencilRuler;
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
  return dateTimeFormatters.logTimestamp.format(date);
};

export const formatDetailTimestamp = (date: Date) => {
  return dateTimeFormatters.full.format(date);
};

export const getHttpMethodVariant = (method: string): "default" | "secondary" => {
  if (method === "POST") return "default";
  return "secondary";
};