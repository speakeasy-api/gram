---
"server": minor
---

The new `workloadIdentities.getCustomFlows` method serves the forms for trusting a platform the Access Hub catalog does not list: registering and editing a trusted platform, and allowing and editing access under one. Each form's title, description, button labels and fields come from a YAML file shipped with the server and are checked when it loads against the management API form they submit, so a form cannot offer a field its request does not accept or let the operator edit a value fixed at registration. The method requires `workload:read`.
