import os
import sys
import json
import requests

# Check for -v flag
verbose = "-v" in sys.argv

# Configuration
server_url = "https://app.getgram.ai"
org_slug = ""  # Change this to your organization slug

url = f"{server_url}/rpc/agents.response"
headers = {
    "Content-Type": "application/json",
    "Gram-Key": os.getenv("GRAM_API_KEY"),
    "Gram-Project": "default",
}

context = f"""Please help me with the following tasks in order:

1. First, get the details of the organization with slug '{org_slug}'
2. Then, find the user associated with the workspaces of the '{org_slug}' organization
3. Please use the email address to search for a stripe customer
4. Then get the stripe charges associated with that customer

Please provide me details on those charges."""

payload = {
    "model": "openai/gpt-4o",
    "instructions": "You are a helpful assistant that can help with Speakeasy operations.",
    "input": context,
    "toolsets": [
        {"toolset_slug": "speakeasy", "environment_slug": "default", "headers": {}},
        {"toolset_slug": "stripe", "environment_slug": "stripe", "headers": {}},
    ],
}
print("=== Turn 1 ===")
resp = requests.post(url, headers=headers, json=payload)
data = resp.json()
print(data["output"][-1]["content"][-1]["text"])

if verbose:
    print("\nFull response:")
    print(json.dumps(data, indent=2))
