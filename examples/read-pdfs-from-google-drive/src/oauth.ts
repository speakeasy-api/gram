import { auth } from "@googleapis/drive";
import { createServer } from "http";
import { parse } from "url";

interface OAuthTokens {
  access_token: string;
  refresh_token: string;
  expiry_date?: number;
}

class GoogleOAuthManager {
  private oauth2Client: any;
  private tokens: OAuthTokens | null = null;
  private readonly REDIRECT_URI = "http://localhost:3000/oauth/callback";
  private readonly SCOPES = ["https://www.googleapis.com/auth/drive.readonly"];

  constructor(clientId: string, clientSecret: string) {
    this.oauth2Client = new auth.OAuth2(
      clientId,
      clientSecret,
      this.REDIRECT_URI
    );
  }

  async getAuthenticatedClient() {
    if (!this.tokens || this.isTokenExpired()) {
      await this.authenticate();
    }

    this.oauth2Client.setCredentials(this.tokens);
    return this.oauth2Client;
  }

  private isTokenExpired(): boolean {
    if (!this.tokens?.expiry_date) return true;
    return Date.now() >= this.tokens.expiry_date;
  }

  private async authenticate(): Promise<void> {
    return new Promise((resolve, reject) => {
      // Create a temporary HTTP server to handle the OAuth callback
      const server = createServer(async (req, res) => {
        const url = parse(req.url!, true);

        if (url.pathname === "/oauth/callback" && url.query["code"]) {
          try {
            const { tokens } = await this.oauth2Client.getToken(
              url.query["code"] as string
            );
            this.tokens = tokens;

            res.writeHead(200, { "Content-Type": "text/html" });
            res.end(`
              <html>
                <body>
                  <h1>✅ Authentication Successful!</h1>
                  <p>You can close this window and return to the terminal.</p>
                </body>
              </html>
            `);

            server.close();
            resolve();
          } catch (error) {
            res.writeHead(500, { "Content-Type": "text/plain" });
            res.end("Authentication failed");
            server.close();
            reject(error);
          }
        } else {
          res.writeHead(404, { "Content-Type": "text/plain" });
          res.end("Not found");
        }
      });

      server.listen(3000, () => {
        const authUrl = this.oauth2Client.generateAuthUrl({
          access_type: "offline",
          scope: this.SCOPES,
          prompt: "consent",
        });

        console.log("\n🔐 Google Drive Authentication Required");
        console.log("Please visit this URL to authorize the application:");
        console.log(authUrl);
        console.log("\nWaiting for authentication...");
      });

      // Timeout after 5 minutes
      setTimeout(() => {
        server.close();
        reject(new Error("Authentication timeout"));
      }, 5 * 60 * 1000);
    });
  }
}

// Global instance
let oauthManager: GoogleOAuthManager | null = null;

export function initializeOAuth(clientId: string, clientSecret: string) {
  oauthManager = new GoogleOAuthManager(clientId, clientSecret);
}

export async function getAuthenticatedClient() {
  if (!oauthManager) {
    throw new Error("OAuth not initialized. Call initializeOAuth first.");
  }
  return await oauthManager.getAuthenticatedClient();
}
