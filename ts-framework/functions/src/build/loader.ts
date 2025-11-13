import { spinner } from "@clack/prompts";

/**
 * Loader wrapper using Clack's spinner, which gracefully handles non-interactive terminals.
 * In CI/CD environments, it falls back to simple text output instead of animations.
 */
export class Loader {
  private spinnerInstance: ReturnType<typeof spinner> | null = null;
  private readonly message: string;

  constructor(message: string) {
    this.message = message;
  }

  /**
   * Start the loader animation.
   */
  start(): void {
    if (this.spinnerInstance) {
      return; // Already running
    }

    this.spinnerInstance = spinner();
    this.spinnerInstance.start(this.message);
  }

  /**
   * Stop the loader animation and clear the line.
   */
  stop(): void {
    if (this.spinnerInstance) {
      this.spinnerInstance.stop();
      this.spinnerInstance = null;
    }
  }
}
