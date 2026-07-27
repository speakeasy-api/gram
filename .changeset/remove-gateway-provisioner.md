---
"server": patch
---

Remove the unused Kubernetes Gateway API custom-domain provisioner. No environment ever enabled it and no cluster has the Gateway API CRDs installed; custom domains are provisioned exclusively through Ingress. This also unblocks the custom-domain health sweep, which failed while trying to list HTTPRoutes on clusters without the Gateway API.
