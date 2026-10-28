"""Render an isolated managed Sandbox probe; performs no cluster writes."""

import argparse
import json
import re


def manifest(namespace, trust_domain, image):
    if not re.fullmatch(
        r"kit-agent-poc-[a-z0-9](?:[a-z0-9-]{0,28}[a-z0-9])?", namespace
    ):
        raise ValueError(
            "namespace must start with kit-agent-poc- and use DNS-label characters"
        )
    if not re.fullmatch(
        r"agents\.global\.(org|proj)-[0-9]+\.system\.id\.goog", trust_domain
    ):
        raise ValueError("expected independently derived agent trust domain")
    if "@sha256:" not in image or not re.fullmatch(
        r"[a-f0-9]{64}", image.split("@sha256:")[-1]
    ):
        raise ValueError(
            "image must be pinned by SHA256 digest and contain /bin/sh, sleep, Python, requests and current google-auth"
        )
    metadata = {"name": "probe", "namespace": namespace}
    pod = {
        "metadata": {
            "labels": {"app": "kit-agent-poc"},
            "annotations": {"iam.gke.io/identity": "spiffe://" + trust_domain + "/*"},
        },
        "spec": {
            "serviceAccountName": "native-agent-probe",
            "automountServiceAccountToken": False,
            "runtimeClassName": "gvisor",
            "restartPolicy": "Always",
            "securityContext": {
                "runAsNonRoot": True,
                "runAsUser": 1000,
                "runAsGroup": 1000,
                "seccompProfile": {"type": "RuntimeDefault"},
            },
            "nodeSelector": {
                "sandbox.gke.io/runtime": "gvisor",
                "workload-type": "sandbox",
            },
            "tolerations": [
                {
                    "key": "sandbox.gke.io/runtime",
                    "operator": "Equal",
                    "value": "gvisor",
                    "effect": "NoSchedule",
                },
                {
                    "key": "workload-type",
                    "operator": "Equal",
                    "value": "sandbox",
                    "effect": "NoSchedule",
                },
            ],
            "containers": [
                {
                    "name": "probe",
                    "image": image,
                    "command": ["/bin/sh", "-c", "sleep 3600"],
                    "securityContext": {
                        "allowPrivilegeEscalation": False,
                        "readOnlyRootFilesystem": True,
                        "capabilities": {"drop": ["ALL"]},
                    },
                    "resources": {
                        "requests": {"cpu": "10m", "memory": "32Mi"},
                        "limits": {"cpu": "100m", "memory": "64Mi"},
                    },
                }
            ],
        },
    }
    return {
        "apiVersion": "v1",
        "kind": "List",
        "items": [
            {
                "apiVersion": "v1",
                "kind": "Namespace",
                "metadata": {"name": namespace, "labels": {"app": "kit-agent-poc"}},
            },
            {
                "apiVersion": "v1",
                "kind": "ServiceAccount",
                "metadata": {
                    "name": "native-agent-probe",
                    "namespace": namespace,
                },
                "automountServiceAccountToken": False,
            },
            {
                "apiVersion": "extensions.agents.x-k8s.io/v1beta1",
                "kind": "SandboxTemplate",
                "metadata": metadata,
                "spec": {
                    "podTemplate": pod,
                    "networkPolicyManagement": "Managed",
                    "networkPolicy": {
                        "ingress": [],
                        "egress": [
                            {
                                "to": [{"ipBlock": {"cidr": "169.254.169.254/32"}}],
                                "ports": [{"protocol": "TCP", "port": 80}],
                            }
                        ],
                    },
                },
            },
            {
                "apiVersion": "extensions.agents.x-k8s.io/v1beta1",
                "kind": "SandboxWarmPool",
                "metadata": metadata,
                "spec": {"replicas": 0, "sandboxTemplateRef": {"name": "probe"}},
            },
            {
                "apiVersion": "extensions.agents.x-k8s.io/v1beta1",
                "kind": "SandboxClaim",
                "metadata": metadata,
                "spec": {"warmPoolRef": {"name": "probe"}},
            },
        ],
    }


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--namespace", required=True)
    parser.add_argument("--trust-domain", required=True)
    parser.add_argument("--image", required=True)
    args = parser.parse_args()
    print(json.dumps(manifest(args.namespace, args.trust_domain, args.image), indent=2))
