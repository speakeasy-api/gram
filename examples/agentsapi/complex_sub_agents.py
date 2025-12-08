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

context = f"""Please help me with the following things
1. Please get the details of the organization and workspaces for the slug '{org_slug}', the users associated with that account
2. For the users you retrieve, please use the email addresses you get to search for stripe customers and get the stripe charges and refunds associated with that customer

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
    "sub_agents": [
        {
            "name": "Speakeasy Sub-Agent",
            "description": "Handles Speakeasy operations like fetching orgs, workspaces, and users.",
            "instructions": """You are a sub-agent that specializes in Speakeasy SDK operations. Use the provided toolsets to fetch organization details, workspaces, and user information as needed.
            When fetching information typically fetch the org first. Then from an org you can fetch workspaces by ID. Users also exist at the workspace level.""",
            "toolsets": [
                {"toolset_slug": "speakeasy", "headers": {}, "environment_slug": "default"},
            ]
        },
        {
            "name": "Stripe Sub-Agent",
            "description": "A sub agent to handle stripe operations like fetching customers, charges, and refunds.",
            "tools": [
                "tools:http:stripe:stripe_get_customers_search",
                "tools:http:stripe:stripe_get_charges",
                "tools:http:stripe:stripe_get_refunds",
            ],
            "environment_slug": "stripe",
        },
    ],
}
resp = requests.post(url, headers=headers, json=payload)
data = resp.json()
print("=== Turn 1 ===")
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
