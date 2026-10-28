import json
import unittest

from render import manifest


class RenderTest(unittest.TestCase):
    def test_isolated_managed(self):
        doc = manifest(
            "kit-agent-poc-test",
            "agents.global.org-123.system.id.goog",
            "example.invalid/probe@sha256:" + "a" * 64,
        )
        ns, sa, template, pool, claim = doc["items"]
        self.assertEqual(ns["metadata"]["name"], "kit-agent-poc-test")
        self.assertEqual(sa["metadata"]["name"], "native-agent-probe")
        self.assertFalse(sa["automountServiceAccountToken"])
        pod = template["spec"]["podTemplate"]
        self.assertFalse(pod["spec"]["automountServiceAccountToken"])
        self.assertEqual(pod["spec"]["serviceAccountName"], sa["metadata"]["name"])
        self.assertEqual(
            pod["metadata"]["annotations"],
            {"iam.gke.io/identity": "spiffe://agents.global.org-123.system.id.goog/*"},
        )
        self.assertEqual(pod["spec"]["runtimeClassName"], "gvisor")
        self.assertEqual(pod["spec"]["nodeSelector"]["workload-type"], "sandbox")
        self.assertEqual(template["spec"]["networkPolicyManagement"], "Managed")
        self.assertEqual(
            template["spec"]["networkPolicy"],
            {
                "ingress": [],
                "egress": [
                    {
                        "to": [{"ipBlock": {"cidr": "169.254.169.254/32"}}],
                        "ports": [{"protocol": "TCP", "port": 80}],
                    }
                ],
            },
        )
        self.assertEqual(pool["spec"]["replicas"], 0)
        self.assertEqual(claim["spec"]["warmPoolRef"]["name"], pool["metadata"]["name"])
        for forbidden in [
            "gcp-service-account",
            "inject-podcertificates",
            "volumes",
            "hostNetwork",
        ]:
            self.assertNotIn(forbidden, json.dumps(doc))

    def test_rejects_unsafe_config(self):
        image = "example.invalid/probe@sha256:" + "a" * 64
        for ns, domain, img in [
            ("default", "agents.global.proj-123.system.id.goog", image),
            ("kit-agent-poc-test", "attacker.invalid", image),
            ("kit-agent-poc-test", "agents.global.proj-123.system.id.goog", "latest"),
        ]:
            with self.assertRaises(ValueError):
                manifest(ns, domain, img)


if __name__ == "__main__":
    unittest.main()
