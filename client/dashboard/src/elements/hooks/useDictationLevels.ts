import { useEffect, useRef, useState } from "react";

/** How fast an un-fed bar level decays each frame (~250ms to silence). */
const DECAY = 0.88;
/** Level injected when the recognizer reports new speech. */
const SPEECH_LEVEL = 1;
/** Idle shimmer so the trail never looks frozen while the mic is open. */
const IDLE_LEVEL = 0.12;

/**
 * Rolling levels for the dictation waveform, newest value last.
 *
 * Deliberately does NOT open its own microphone stream. An analyser-driven
 * version reads true amplitude, but the second `getUserMedia` capture competes
 * with the Web Speech recognizer for the device — on macOS Chrome that makes
 * the recognizer drop most of an utterance and finalize only the last word.
 * Speech activity comes from the recognizer's own transcript updates instead:
 * the bars rise while words are arriving and decay when speech pauses.
 *
 * @param transcript the live interim transcript; any change counts as speech
 * @param sampleCount number of bars to keep in the trail
 */
export function useDictationLevels(
  transcript: string | undefined,
  sampleCount: number,
): number[] {
  const emptyTrail = () => Array.from<number>({ length: sampleCount }).fill(0);
  const [levels, setLevels] = useState<number[]>(emptyTrail);
  const historyRef = useRef<number[]>(emptyTrail());
  // Read inside the animation frame so a transcript change doesn't restart it.
  const transcriptRef = useRef(transcript);
  const lastSeenRef = useRef(transcript);
  transcriptRef.current = transcript;

  useEffect(() => {
    let frame: number | undefined;
    let level = 0;
    let phase = 0;

    const tick = () => {
      phase += 1;
      if (transcriptRef.current !== lastSeenRef.current) {
        lastSeenRef.current = transcriptRef.current;
        level = SPEECH_LEVEL;
      } else {
        level = Math.max(IDLE_LEVEL, level * DECAY);
      }
      // Jitter on the frame counter, not the level: keyed off `level` alone the
      // idle floor is a constant, so the trail froze into a flat line a second
      // into any silence while the mic was still listening.
      const jittered = level * (0.55 + 0.45 * Math.abs(Math.sin(phase * 0.37)));
      historyRef.current = [...historyRef.current.slice(1), jittered];
      setLevels(historyRef.current);
      frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);

    return () => {
      if (frame !== undefined) cancelAnimationFrame(frame);
    };
  }, [sampleCount]);

  return levels;
}
