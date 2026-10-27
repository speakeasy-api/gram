#!/usr/bin/env python3
"""Render an isolated managed Sandbox probe; performs no cluster writes."""

import argparse
import json
import re


def manifest(namespace, iam_email, image):
    if not re.fullmatch(r"kit-wif-poc-[a-z0-9-]{1,30}", namespace):
        raise ValueError(
            "namespace must start with kit-wif-poc- and use DNS-label characters"
        )
    if not re.fullmatch(r"[a-z0-9-]+@[a-z0-9-]+\.iam\.gserviceaccount\.com", iam_email):
        raise ValueError("expected IAM service-account email")
    if "@sha256:" not in image or not re.fullmatch(
        r"[a-f0-9]{64}", image.split("@sha256:")[-1]
    ):
        raise ValueError(
            "image must be pinned by SHA256 digest and contain /bin/sh and curl"
        )
    metadata = {"name": "probe", "namespace": namespace}
    pod = {
        "metadata": {"labels": {"app": "kit-wif-poc"}},
        "spec": {
            "serviceAccountName": "probe",
            "automountServiceAccountToken": False,
            "runtimeClassName": "gvisor",
            "restartPolicy": "Always",
            "securityContext": {
                "runAsNonRoot": True,
                "runAsUser": 1000,
                "runAsGroup": 1000,
                "seccompProfile": {"type": "RuntimeDefault"},
            },
            "nodeSelector": {"sandbox.gke.io/runtime": "gvisor"},
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
                "metadata": {"name": namespace, "labels": {"app": "kit-wif-poc"}},
            },
            {
                "apiVersion": "v1",
                "kind": "ServiceAccount",
                "metadata": {
                    **metadata,
                    "annotations": {"iam.gke.io/gcp-service-account": iam_email},
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
    parser.add_argument("--iam-email", required=True)
    parser.add_argument("--image", required=True)
    args = parser.parse_args()
    print(json.dumps(manifest(args.namespace, args.iam_email, args.image), indent=2))
