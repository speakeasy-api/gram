import { useEffect, useState } from "react";

export type DeploymentStatus = "none" | "processing" | "complete" | "error";

export interface CliState {
  sessionToken: string;
  deploymentStatus: DeploymentStatus;
  logs: Array<{ id: string; timestamp: number; message: string; type: "info" | "error" | "success"; loading?: boolean }>;
}

// Mock polling for now - replace with real API call later
export function useCliConnection() {
  const [state, setState] = useState<CliState>({
    sessionToken: generateSessionToken(),
    deploymentStatus: "none",
    logs: [],
  });

  useEffect(() => {
    // Mock: detect deployment after 3 seconds (simulates user uploading via CLI)
    const deploymentDetectedTimer = setTimeout(() => {
      setState(prev => ({
        ...prev,
        deploymentStatus: "processing",
        logs: [...prev.logs,
          {
            id: "deployment-detected",
            timestamp: Date.now(),
            message: "Deployment detected",
            type: "info"
          },
          {
            id: "deployment-status",
            timestamp: Date.now(),
            message: "Processing deployment...",
            type: "info",
            loading: true
          }
        ],
      }));
    }, 3000);

    // Mock: deployment completes after 6 seconds total
    const deploymentCompleteTimer = setTimeout(() => {
      setState(prev => ({
        ...prev,
        deploymentStatus: "complete",
        logs: prev.logs.map(log =>
          log.id === "deployment-status"
            ? { ...log, message: "✓ Deployment complete", type: "success" as const, loading: false }
            : log
        ),
      }));
    }, 6000);

    return () => {
      clearTimeout(deploymentDetectedTimer);
      clearTimeout(deploymentCompleteTimer);
    };
  }, []);

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
