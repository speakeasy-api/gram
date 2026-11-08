// Minimal client for TypeSafe's System One ("Jev") judgement API as hosted by
// OpenRouter. Mirrors server/internal/thirdparty/typesafe/client.go: an
// opaque state document plus named questions in, a probability per question
// out. Kept dependency-free so the pure parts of the harness run under
// `node --test` without a browser or a stack.

export const DEFAULT_MODEL = "typesafe/jev-1.13";
export const DEFAULT_ENDPOINT = "https://openrouter.ai/api/v1/systemone";

// One judgement is a few seconds; the launcher budgets 4s and the prompt
// injection prefilter 10s. A UI case is not latency sensitive, so take the
// looser of the two.
export const DEFAULT_TIMEOUT_MS = 10_000;

export type Question = {
  type: "choice" | "noul";
  instructions: string;
  // For "noul" exactly the keys "true" and "false".
  criteria: Record<string, string>;
};

export type JevRequest = {
  model: string;
  state: unknown;
  questions: Record<string, Question>;
};

export type Answer = {
  type: "choice" | "noul";
  choice?: string;
  probabilities?: Record<string, number>;
  noul?: number;
};

export type JevResponse = {
  model: string;
  answers: Record<string, Answer>;
  usage: { input_tokens: number; output_tokens: number };
};

export type JevResult = { response: JevResponse; latencyMs: number };

export class JevError extends Error {
  readonly status: number | undefined;

  constructor(message: string, status?: number) {
    super(message);
    this.name = "JevError";
    this.status = status;
  }
}

export type JevClientOptions = {
  apiKey: string;
  endpoint?: string;
  model?: string;
  timeoutMs?: number;
  fetch?: typeof fetch;
};

export function buildRequest(
  state: unknown,
  questions: Record<string, Question>,
  model = DEFAULT_MODEL,
): JevRequest {
  for (const [id, q] of Object.entries(questions)) {
    if (q.type === "noul") {
      const keys = Object.keys(q.criteria).sort();
      if (keys.length !== 2 || keys[0] !== "false" || keys[1] !== "true") {
        throw new Error(
          `question ${id}: noul criteria must be exactly "true" and "false"`,
        );
      }
    } else if (Object.keys(q.criteria).length < 2) {
      throw new Error(`question ${id}: choice needs at least two criteria`);
    }
  }
  return { model, state, questions };
}

// Decodes a 200 body. Every requested question must be answered with the
// type it was asked as, so a partial answer is an error rather than a case
// that quietly passes with no probability.
export function decodeResponse(
  body: unknown,
  questions: Record<string, Question>,
): JevResponse {
  if (typeof body !== "object" || body === null) {
    throw new JevError("response is not an object");
  }
  const r = body as Partial<JevResponse>;
  if (
    typeof r.model !== "string" ||
    typeof r.answers !== "object" ||
    !r.answers
  ) {
    throw new JevError("response lacks model or answers");
  }
  for (const [id, q] of Object.entries(questions)) {
    const a = r.answers[id];
    if (!a || a.type !== q.type) {
      throw new JevError(`answer ${id} missing or of the wrong type`);
    }
    if (q.type === "noul" && typeof a.noul !== "number") {
      throw new JevError(`answer ${id} has no noul probability`);
    }
    if (
      q.type === "choice" &&
      (typeof a.choice !== "string" || !a.probabilities)
    ) {
      throw new JevError(`answer ${id} has no choice or probabilities`);
    }
  }
  const usage = r.usage ?? { input_tokens: 0, output_tokens: 0 };
  return { model: r.model, answers: r.answers, usage };
}

export class JevClient {
  private readonly apiKey: string;
  private readonly endpoint: string;
  private readonly model: string;
  private readonly timeoutMs: number;
  private readonly fetchImpl: typeof fetch;

  constructor(opts: JevClientOptions) {
    if (!opts.apiKey) throw new JevError("api key is not configured");
    this.apiKey = opts.apiKey;
    this.endpoint = opts.endpoint ?? DEFAULT_ENDPOINT;
    this.model = opts.model ?? DEFAULT_MODEL;
    this.timeoutMs = opts.timeoutMs ?? DEFAULT_TIMEOUT_MS;
    this.fetchImpl = opts.fetch ?? fetch;
  }

  async ask(
    state: unknown,
    questions: Record<string, Question>,
  ): Promise<JevResult> {
    const req = buildRequest(state, questions, this.model);
    const started = performance.now();
    let res: Response;
    try {
      res = await this.fetchImpl(this.endpoint, {
        method: "POST",
        headers: {
          Authorization: `Bearer ${this.apiKey}`,
          "Content-Type": "application/json",
          Accept: "application/json",
        },
        body: JSON.stringify(req),
        signal: AbortSignal.timeout(this.timeoutMs),
      });
    } catch (cause) {
      throw new JevError(`transport: ${(cause as Error).message}`);
    }
    const latencyMs = Math.round(performance.now() - started);
    if (res.status !== 200) {
      const text = (await res.text()).slice(0, 512);
      throw new JevError(`upstream status ${res.status}: ${text}`, res.status);
    }
    const body: unknown = await res.json();
    return { response: decodeResponse(body, questions), latencyMs };
  }
}
