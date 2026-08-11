import type { DictationAdapter } from "@assistant-ui/react";

const getSpeechRecognition = ():
  | (new () => SpeechRecognitionLike)
  | undefined =>
  typeof window === "undefined"
    ? undefined
    : (window.SpeechRecognition ?? window.webkitSpeechRecognition);

/** The slice of the Web Speech API this adapter uses. */
interface SpeechRecognitionLike extends EventTarget {
  lang: string;
  continuous: boolean;
  interimResults: boolean;
  start(): void;
  stop(): void;
  abort(): void;
}

interface SpeechAlternativeLike {
  transcript: string;
}
interface SpeechResultLike {
  isFinal: boolean;
  0?: SpeechAlternativeLike;
}
interface SpeechResultEventLike extends Event {
  resultIndex: number;
  results: ArrayLike<SpeechResultLike>;
}

/**
 * Dictation over the Web Speech API that reports the WHOLE utterance on every
 * update, rather than per-segment deltas.
 *
 * assistant-ui's bundled `WebSpeechDictationAdapter` emits each segment once
 * and relies on the composer to concatenate them. In Chrome that accumulation
 * doesn't survive: an interim arriving after a finalized segment resets the
 * composer's base text, so the draft ends up holding only the trailing
 * fragment — dictate a sentence, send it, and the message is its last word.
 *
 * Emitting cumulative text sidesteps the whole question of who accumulates.
 * Everything spoken so far is sent as one interim result, so the draft is
 * correct after every event; a single final at the end commits it. Any text
 * the user typed before starting is preserved by the composer, which snapshots
 * it as the base and appends what we emit.
 */
interface DictationOptions {
  language?: string;
  continuous?: boolean;
  interimResults?: boolean;
}

class CumulativeWebSpeechDictationAdapter implements DictationAdapter {
  // Plain field + assignment rather than a parameter property: the dashboard
  // compiles with `erasableSyntaxOnly`, which bans TS-only constructor sugar.
  private readonly options: DictationOptions;

  constructor(options: DictationOptions = {}) {
    this.options = options;
  }

  static isSupported(): boolean {
    return getSpeechRecognition() !== undefined;
  }

  listen(): DictationAdapter.Session {
    const Recognition = getSpeechRecognition();
    if (!Recognition) {
      throw new Error("SpeechRecognition is not supported in this browser.");
    }

    const recognition = new Recognition();
    recognition.lang = this.options.language ?? "en-US";
    recognition.continuous = this.options.continuous ?? true;
    recognition.interimResults = this.options.interimResults ?? true;

    const speechStartCallbacks = new Set<() => void>();
    const speechEndCallbacks = new Set<(r: DictationAdapter.Result) => void>();
    const speechCallbacks = new Set<(r: DictationAdapter.Result) => void>();

    /** Finalized segments, keyed by index so a revised segment replaces rather
     *  than duplicates — Chrome re-emits a segment when it corrects it. */
    const finals = new Map<number, string>();
    let interim = "";

    const cumulative = () => {
      const finalized = [...finals.entries()]
        .sort(([a], [b]) => a - b)
        .map(([, text]) => text)
        .join("");
      return (finalized + interim).trim();
    };

    const session: DictationAdapter.Session = {
      status: { type: "starting" },
      stop: async () => {
        recognition.stop();
        return new Promise<void>((resolve) => {
          const check = () => {
            if (session.status.type === "ended") resolve();
            else setTimeout(check, 50);
          };
          check();
        });
      },
      cancel: () => recognition.abort(),
      onSpeechStart: (callback) => {
        speechStartCallbacks.add(callback);
        return () => {
          speechStartCallbacks.delete(callback);
        };
      },
      onSpeechEnd: (callback) => {
        speechEndCallbacks.add(callback);
        return () => {
          speechEndCallbacks.delete(callback);
        };
      },
      onSpeech: (callback) => {
        speechCallbacks.add(callback);
        return () => {
          speechCallbacks.delete(callback);
        };
      },
    };

    recognition.addEventListener("start", () => {
      session.status = { type: "running" };
    });
    recognition.addEventListener("speechstart", () => {
      for (const cb of speechStartCallbacks) cb();
    });

    recognition.addEventListener("result", (event) => {
      const speechEvent = event as SpeechResultEventLike;
      interim = "";
      for (
        let i = speechEvent.resultIndex;
        i < speechEvent.results.length;
        i++
      ) {
        const result = speechEvent.results[i];
        if (!result) continue;
        const transcript = result[0]?.transcript ?? "";
        if (result.isFinal) finals.set(i, transcript);
        else interim += transcript;
      }
      // Always interim: the composer treats a final as "append this delta",
      // which would duplicate everything we have already reported.
      for (const cb of speechCallbacks)
        cb({ transcript: cumulative(), isFinal: false });
    });

    recognition.addEventListener("end", () => {
      if (session.status.type !== "ended") {
        session.status = { type: "ended", reason: "stopped" };
      }
      const transcript = cumulative();
      if (transcript) {
        for (const cb of speechEndCallbacks) cb({ transcript });
      }
    });

    recognition.addEventListener("error", (event) => {
      const { error } = event as Event & { error?: string };
      session.status = {
        type: "ended",
        reason: error === "aborted" ? "cancelled" : "error",
      };
      if (error && error !== "aborted" && error !== "no-speech") {
        console.error("Dictation error:", error);
      }
    });

    recognition.start();
    return session;
  }
}

/**
 * Push-to-talk dictation, or `undefined` where the browser has no Web Speech
 * API (Firefox) so the composer can hide the mic instead of offering a button
 * that throws.
 */
export const dictationAdapter =
  CumulativeWebSpeechDictationAdapter.isSupported()
    ? new CumulativeWebSpeechDictationAdapter({
        language: "en-US",
        continuous: true,
        interimResults: true,
      })
    : undefined;
