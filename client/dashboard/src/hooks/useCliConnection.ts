import { useEffect, useState } from "react";
import { useListTools } from "./toolTypes";

export type DeploymentStatus = "none" | "processing" | "complete" | "error";

export interface CliState {
  sessionToken: string;
  deploymentStatus: DeploymentStatus;
  logs: Array<{
    id: string;
    timestamp: number;
    message: string;
    type: "info" | "error" | "success";
    loading?: boolean;
  }>;
  connected: boolean;
}

export function useCliConnection() {
  const [state, setState] = useState<CliState>({
    sessionToken: generateSessionToken(),
    deploymentStatus: "none",
    logs: [],
    connected: false,
  });

  const { data: tools } = useListTools(undefined, undefined, {
    refetchInterval: state.deploymentStatus !== "complete" ? 2000 : false,
  });

  // Start the animation immediately on mount
  useEffect(() => {
    // Step 1: Show auth command
    setState((prev) => ({
      ...prev,
      logs: [
        {
          id: "auth-cmd",
          timestamp: Date.now(),
          message: "$ gram auth",
          type: "info",
        },
      ],
    }));

    // Step 2: Show auth success after command is typed (1s)
    const timer1 = setTimeout(() => {
      setState((prev) => ({
        ...prev,
        logs: [
          ...prev.logs,
          {
            id: "auth-success",
            timestamp: Date.now(),
            message: "Authentication successful",
            type: "success",
          },
        ],
      }));
    }, 1000);

    // Step 3: Show upload command (after 1.5s total)
    const timer2 = setTimeout(() => {
      setState((prev) => ({
        ...prev,
        logs: [
          ...prev.logs,
          {
            id: "upload-cmd",
            timestamp: Date.now(),
            message:
              '$ gram upload --type function --location ./functions.zip --name "My Functions" --slug my-functions --runtime nodejs:22',
            type: "info",
          },
        ],
      }));
    }, 1500);

    // Step 4: Show uploading assets status after upload command is typed (after 3.5s total)
    const timer3 = setTimeout(() => {
      setState((prev) => ({
        ...prev,
        logs: [
          ...prev.logs,
          {
            id: "upload-status",
            timestamp: Date.now(),
            message: "uploading assets",
            type: "info",
            loading: true,
          },
        ],
      }));
    }, 3500);

    return () => {
      clearTimeout(timer1);
      clearTimeout(timer2);
      clearTimeout(timer3);
    };
  }, []);

  // Check for tools to determine when deployment is complete
  useEffect(() => {
    const hasTools = tools?.tools && tools.tools.length > 0;

    if (hasTools && state.deploymentStatus === "none") {
      setState((prev) => ({
        ...prev,
        deploymentStatus: "processing",
        connected: true,
      }));

      setTimeout(() => {
        setState((prev) => ({
          ...prev,
          deploymentStatus: "complete",
          logs: prev.logs.map((log) =>
            log.id === "upload-status"
              ? {
                  ...log,
                  message: "upload success",
                  type: "success" as const,
                  loading: false,
                }
              : log,
          ),
        }));
      }, 500);
    }
  }, [tools, state.deploymentStatus]);

  return state;
}

function generateSessionToken(): string {
  const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789";
  let token = "";
  for (let i = 0; i < 3; i++) {
    for (let j = 0; j < 3; j++) {
      token += chars.charAt(Math.floor(Math.random() * chars.length));
    }
    if (i < 2) token += "-";
  }
  return token;
}
