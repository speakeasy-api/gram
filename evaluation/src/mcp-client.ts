import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";
import type { MCPServerConfig } from "./types.ts";

/**
 * Manages connection to an MCP server
 */
export class MCPClient {
  private client: Client;
  private transport: StdioClientTransport;
  private connected = false;

  constructor(config: MCPServerConfig) {
    this.client = new Client(
      {
        name: "gram-evaluation-client",
        version: "1.0.0",
      },
      {
        capabilities: {
          tools: {},
        },
      },
    );

    this.transport = new StdioClientTransport({
      command: config.command,
      args: config.args,
      env: config.env,
    });
  }

  /**
   * Connect to the MCP server
   */
  async connect(): Promise<void> {
    if (this.connected) {
      return;
    }

    await this.client.connect(this.transport);
    this.connected = true;
  }

  /**
   * List all available tools from the MCP server
   */
  async listTools(): Promise<any[]> {
    if (!this.connected) {
      throw new Error("Not connected to MCP server");
    }

    const response = await this.client.listTools();
    return response.tools;
  }

  /**
   * Call a tool on the MCP server
   */
  async callTool(name: string, args: any): Promise<any> {
    if (!this.connected) {
      throw new Error("Not connected to MCP server");
    }

    const response = await this.client.callTool({ name, arguments: args });
    return response;
  }

  /**
   * Close the connection to the MCP server
   */
  async close(): Promise<void> {
    if (!this.connected) {
      return;
    }

    await this.client.close();
    this.connected = false;
  }

  /**
   * Get the underlying client for advanced usage
   */
  getClient(): Client {
    return this.client;
  }
}
