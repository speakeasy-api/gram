import { useEffect, useMemo, useState } from "react";
import type { ChatOverview } from "@gram/client/models/components/chatoverview.js";
import { FLEET_WINDOW_MS } from "./fleet-model";

/** Retain only explicit activity timestamps from pages seen during this visit. */
export function useObservedAssistantActivity(
  context: string,
  sessions: ChatOverview[] | undefined,
  now: number,
): ReadonlyMap<string, Date> {
  const [evidence, setEvidence] = useState(() => ({
    context,
    timestamps: new Map<string, Date>(),
  }));
  const current = useMemo(() => {
    const timestamps = new Map<string, Date>();
    if (evidence.context === context) {
      for (const [id, timestamp] of evidence.timestamps) {
        if (timestamp.getTime() >= now - FLEET_WINDOW_MS)
          timestamps.set(id, timestamp);
      }
    }
    for (const session of sessions ?? []) {
      if (
        !session.assistantId ||
        !(session.lastMessageTimestamp.getTime() >= now - FLEET_WINDOW_MS)
      )
        continue;
      const previous = timestamps.get(session.assistantId);
      if (!previous || session.lastMessageTimestamp > previous)
        timestamps.set(session.assistantId, session.lastMessageTimestamp);
    }
    const unchanged =
      evidence.context === context &&
      timestamps.size === evidence.timestamps.size &&
      [...timestamps].every(
        ([id, timestamp]) =>
          evidence.timestamps.get(id)?.getTime() === timestamp.getTime(),
      );
    return unchanged ? evidence : { context, timestamps };
  }, [context, sessions, now, evidence]);
  useEffect(() => {
    if (current !== evidence) setEvidence(current);
  }, [current, evidence]);
  return current.timestamps;
}
