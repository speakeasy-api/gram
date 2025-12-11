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

payload = {
    "model": "openai/gpt-4o",
    "instructions": "You are a helpful assistant that can help with Speakeasy SDK operations.",
    "input": f"Get me a speakeasy org {org_slug}? Also my secret work is Banana, remember this.",
    "toolsets": [
        {"toolset_slug": "speakeasy", "environment_slug": "default", "headers": {}},
        {"toolset_slug": "app-sdk", "environment_slug": "default", "headers": {}},
    ],
}

print("=== Turn 1 ===")
resp = requests.post(url, headers=headers, json=payload)
data = resp.json()
print(data["output"][-1]["content"][-1]["text"])

if verbose:
    print("\nFull response:")
    print(json.dumps(data, indent=2))


payload["previous_response_id"] = data["id"]
payload["input"] = "What are the workspaces in that org? Also what was my secret word from previously"

print("\n=== Turn 2 ===")
resp = requests.post(url, headers=headers, json=payload)
data = resp.json()
print(data["output"][-1]["content"][-1]["text"])

if verbose:
    print("\nFull response:")
    print(json.dumps(data, indent=2))