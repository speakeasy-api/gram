---
"server": minor
---

Private network ingresses can run highly available. New opt-in settings run several Tailscale proxy and attestor replicas per organization, pin proxies to a shared ProxyClass, schedule attestors onto a dedicated node pool, and keep one replica of each available during node drains with PodDisruptionBudgets. An ingress now reports online while any proxy and attestor replica is serving, so a rolling update no longer shows it as pending. With the settings unset, every resource keeps its current single-replica shape.
