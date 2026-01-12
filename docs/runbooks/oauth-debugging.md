# Debugging MCP OAuth Flows

To debug MCP OAuth flows with your local Gram dev server, you'll need to expose
your local server via ngrok so that external OAuth providers can redirect back
to your machine.

## Setup

1. Get setup with ngrok (ask to be added to our team account if needed)

2. Create a fixed domain like `gram-yourname.ngrok.app` on the
   [ngrok domains page](https://dashboard.ngrok.com/domains)

3. Configure your local environment to use the ngrok URL:
   ```bash
   mise set --file mise.local.toml GRAM_SERVER_URL=https://gram-yourname.ngrok.app
   ```

## Running the Dev Server

You have two options:

### Option A: Run with Zero

Run your dev server with zero for the standard development experience.

### Option B: Run with IDE Debugger

Run the debugger in your IDE so you can add breakpoints, and run
`mise start:dashboard` in a separate terminal.

## Start the ngrok Tunnel

In another terminal, start the ngrok tunnel:

```bash
ngrok http --url=gram-yourname.ngrok.app https://localhost:8080
```

## Test the OAuth Flow

Add a Gram MCP server that uses OAuth to your desired app (Claude Code, Cursor,
or any other MCP client) and test the authentication flow.
