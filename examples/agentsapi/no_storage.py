import os
import sys
import json
import time
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
    "input": f"Get me a speakeasy org {org_slug}?",
    "store": False,
    "toolsets": [
        {"toolset_slug": "speakeasy", "environment_slug": "default", "headers": {}},
        {"toolset_slug": "app-sdk", "environment_slug": "default", "headers": {}},
    ],
}

print("=== Turn 1 ===")
resp = requests.post(url, headers=headers, json=payload)
data = resp.json()
response_id = data["id"]
print(data["output"][-1]["content"][-1]["text"])

# There is a delay of up to a minute befor a response is deleted on completion
time.sleep(30)

if verbose:
    print("\nFull response:")
    print(json.dumps(data, indent=2))

poll_url = f"{server_url}/rpc/agents.response?response_id={response_id}"
try:
    poll_resp = requests.get(poll_url, headers=headers)
    poll_resp.raise_for_status()
except requests.exceptions.HTTPError as e:
    print(f"\nExpected error: {e}")


