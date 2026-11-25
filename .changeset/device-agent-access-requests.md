---
"server": minor
---

Organization administrators can now see pending access requests in the device agent. `agent.listPendingAccessRequests` lists the organization's pending requests, newest first, each with a link that opens it in the dashboard to approve or deny; anyone who is not an administrator gets an empty list. It accepts only a per-user device agent key and is off unless access requests are enabled for the organization. Access requests can now also carry a suggestion of how long the requester needs the access, from the dashboard request form: a duration counted from approval (1 hour, 2 hours or 4 hours), or a deadline such as the end of their day, sent as `requested_until` and stored in UTC. The Slack card shows it ("Asked for 2 hours" or "Asked until" the requested time) and opens its time menu on the matching choice, and approving End of day on a request with a deadline gives access until that deadline.
