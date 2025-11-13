/**
 * Simple ASCII rotating square loader animation for terminal output.
 * Displays a rotating square outline with a message like: ◰ deploying function...
 */
export class Loader {
  private interval: NodeJS.Timeout | null = null;
  private frame = 0;
  private readonly frames = ["◰", "◳", "◲", "◱"];
  private readonly message: string;

  constructor(message: string) {
    this.message = message;
  }

  /**
   * Start the loader animation.
   */
  start(): void {
    if (this.interval) {
      return; // Already running
    }

    // Hide cursor
    process.stdout.write("\x1b[?25l");

    // Render first frame immediately
    this.render();

    // Update every 100ms
    this.interval = setInterval(() => {
      this.frame = (this.frame + 1) % this.frames.length;
      this.render();
    }, 100);
  }

  /**
   * Stop the loader animation and clear the line.
   */
  stop(): void {
    if (this.interval) {
      clearInterval(this.interval);
      this.interval = null;
    }

    // Clear the line and show cursor
    process.stdout.write("\r\x1b[K");
    process.stdout.write("\x1b[?25h");
  }

  private render(): void {
    const spinner = this.frames[this.frame];
    process.stdout.write(`\r${spinner} ${this.message}`);
  }
}
