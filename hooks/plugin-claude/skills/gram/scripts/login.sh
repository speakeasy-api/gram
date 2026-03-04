#!/usr/bin/env bash

# Setup Claude settings directory
CLAUDE_DIR="$HOME/.claude"
CLAUDE_SETTINGS="$CLAUDE_DIR/settings.json"

mkdir -p "$CLAUDE_DIR"

# Find an available port
PORT=$((8000 + RANDOM % 1000))
while lsof -Pi :$PORT -sTCP:LISTEN -t >/dev/null 2>&1; do
  PORT=$((PORT + 1))
done

# Determine server URL
SERVER_URL="https://app.getgram.ai"
if [ -n "$GRAM_SERVER_URL" ]; then
  SERVER_URL="$GRAM_SERVER_URL"
fi

# Create temp files for capturing response
RESPONSE_FILE=$(mktemp)
trap "rm -f $RESPONSE_FILE" EXIT

# Start temporary HTTP server to receive callback using Python
python3 -c "
import http.server
import socketserver
import urllib.parse
import sys
import os

response_file = '$RESPONSE_FILE'
port = $PORT

class CallbackHandler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        # Parse query parameters
        parsed = urllib.parse.urlparse(self.path)
        params = urllib.parse.parse_qs(parsed.query)

        api_key = params.get('api_key', [''])[0]
        project = params.get('project', ['default'])[0]

        if api_key:
            # Write to response file
            with open(response_file, 'w') as f:
                f.write(api_key + '\n')
                f.write(project + '\n')

            # Send success response
            self.send_response(200)
            self.send_header('Content-type', 'text/html')
            self.end_headers()
            html = '''<!DOCTYPE html>
<html>
<head><title>Authentication Successful</title></head>
<body style=\"font-family: system-ui; text-align: center; padding: 50px;\">
  <h1>Authentication Successful!</h1>
  <p>You can close this window and return to your terminal.</p>
</body>
</html>'''
            self.wfile.write(html.encode())

            # Exit after successful response
            sys.exit(0)

    def log_message(self, format, *args):
        pass  # Suppress logs

with socketserver.TCPServer(('127.0.0.1', port), CallbackHandler) as httpd:
    httpd.serve_forever()
" &
SERVER_PID=$!

# Give server a moment to start
sleep 0.5

# URL-encode the callback URL
CALLBACK_URL="http://localhost:$PORT"
ENCODED_CALLBACK=$(python3 -c "import urllib.parse; print(urllib.parse.quote('$CALLBACK_URL', safe=''))")

# Open browser to login page with callback
LOGIN_URL="$SERVER_URL/hooks-login?callback=$ENCODED_CALLBACK"

if command -v open >/dev/null 2>&1; then
  open "$LOGIN_URL" 2>/dev/null
elif command -v xdg-open >/dev/null 2>&1; then
  xdg-open "$LOGIN_URL" 2>/dev/null
elif command -v wslview >/dev/null 2>&1; then
  wslview "$LOGIN_URL" 2>/dev/null
else
  echo "Please open this URL in your browser:"
  echo "$LOGIN_URL"
fi

# Wait for response with timeout (120 seconds)
COUNTER=0
while [ ! -s "$RESPONSE_FILE" ] && [ $COUNTER -lt 120 ]; do
  sleep 1
  COUNTER=$((COUNTER + 1))
done

# Kill the server
kill $SERVER_PID 2>/dev/null
wait $SERVER_PID 2>/dev/null

# Check if we got a response
if [ ! -s "$RESPONSE_FILE" ]; then
  echo "❌ Authentication timed out. Please try again."
  exit 1
fi

# Read the API key and project from response
GRAM_API_KEY=$(sed -n '1p' "$RESPONSE_FILE")
GRAM_PROJECT=$(sed -n '2p' "$RESPONSE_FILE")

if [ -z "$GRAM_API_KEY" ]; then
  echo "❌ Failed to receive API key. Please try again."
  exit 1
fi

# Create or update settings.json with env variables
if [ ! -f "$CLAUDE_SETTINGS" ]; then
  cat > "$CLAUDE_SETTINGS" << EOF
{
  "env": {
    "GRAM_API_KEY": "$GRAM_API_KEY",
    "GRAM_PROJECT": "$GRAM_PROJECT"
  }
}
EOF
else
  # Use jq to update existing settings.json if available
  if command -v jq >/dev/null 2>&1; then
    TMP_FILE=$(mktemp)
    jq --arg key "$GRAM_API_KEY" --arg project "$GRAM_PROJECT" \
      '.env = (.env // {}) | .env.GRAM_API_KEY = $key | .env.GRAM_PROJECT = $project' \
      "$CLAUDE_SETTINGS" > "$TMP_FILE" && mv "$TMP_FILE" "$CLAUDE_SETTINGS"
  else
    echo ""
    echo "⚠️  jq not found. Please manually add the following to $CLAUDE_SETTINGS then restart Claude Code:"
    echo ""
    echo '  "env": {'
    echo '    "GRAM_API_KEY": "'"$GRAM_API_KEY"'",'
    echo '    "GRAM_PROJECT": "'"$GRAM_PROJECT"'"'
    echo '  }'
    echo ""
    exit 1
  fi
fi

echo "✅ Successfully authenticated with Gram!"
echo "YOU MUST RESTART CLAUDE FOR THE CHANGE TO TAKE EFFECT"
