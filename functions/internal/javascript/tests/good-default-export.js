class Tools {
  /**
   * @param {{"name": "greet", input: {"user": string}}} call
   */
  async handleToolCall(call) {
    const { name, input } = call;
    if (name !== "greet") {
      throw new Error(`Unknown tool: ${name}`);
    }
    return new Response(JSON.stringify({ message: `Hello, ${input.user}!` }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  }
}

const tools = new Tools();
export default tools;
