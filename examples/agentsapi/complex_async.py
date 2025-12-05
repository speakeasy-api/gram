import os
import sys
import json
import time
import requests

# Check for -v flag
verbose = "-v" in sys.argv

# Configuration
server_url = "https://app.getgram.ai"
org_slug = ""

url = f"{server_url}/rpc/agents.response"
headers = {
    "Content-Type": "application/json",
    "Gram-Key": os.getenv("GRAM_API_KEY"),
    "Gram-Project": "default",
}

context = f"""Please help me with the following things
1. Please get the details of the organization with slug '{org_slug}' and get all the workspaces for it
2. Then, find the user associated with the workspaces of the '{org_slug}' organization
3. Please use the email address to search for a stripe customer and get the stripe charges and refunds associated with that customer

Structure your final result like:
PLAN
- please summarize the origin plan you created
SUMMARY
- please return all the data to me organized and summarized well with key insights."""

payload = {
    "model": "openai/gpt-4o",
    "async": True,
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
response_id = data["id"]
print(f"Response ID: {response_id}")

# Poll for completion
poll_url = f"{server_url}/rpc/agents.response?response_id={response_id}"
while True:
    time.sleep(5)
    poll_resp = requests.get(poll_url, headers=headers)
    poll_data = poll_resp.json()
    status = poll_data.get("status")
    print(f"Status: {status}")
    
    if status != "in_progress":
        if "output" in poll_data and poll_data["output"]:
            for item in poll_data["output"]:
                if item.get("role") == "assistant":
                    print("\n=== Plan ===")
                    print(item["content"][-1]["text"])
                    break

            print("\n=== Final Response ===")
            print(poll_data["output"][-1]["content"][-1]["text"])
        
        if verbose:
            print("\nFull response:")
            print(json.dumps(poll_data, indent=2))
        break
