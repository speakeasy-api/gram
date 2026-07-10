import * as z from "zod/v4-mini";
export interface Env {
    GRAM_DEBUG?: boolean | undefined;
}
export declare const envSchema: z.ZodMiniType<Env, unknown>;
/**
 * Reads and validates environment variables.
 */
export declare function env(): Env;
/**
 * Clears the cached env object. Useful for testing with a fresh environment.
 */
export declare function resetEnv(): void;
//# sourceMappingURL=env.d.ts.map