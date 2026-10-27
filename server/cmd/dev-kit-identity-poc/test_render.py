import unittest

from render import manifest


class ManifestTests(unittest.TestCase):
    def test_isolated_workload(self):
        doc = manifest(
            "kit-wif-poc-test",
            "probe@example-project.iam.gserviceaccount.com",
            "example.invalid/probe@sha256:" + "a" * 64,
        )
        ns, sa, template, pool, claim = doc["items"]
        self.assertEqual(ns["metadata"]["name"], "kit-wif-poc-test")
        self.assertFalse(sa["automountServiceAccountToken"])
        spec = template["spec"]["podTemplate"]["spec"]
        self.assertFalse(spec["automountServiceAccountToken"])
        self.assertNotIn("volumes", spec)
        self.assertEqual(spec["runtimeClassName"], "gvisor")
        self.assertEqual(spec["serviceAccountName"], sa["metadata"]["name"])
        self.assertEqual(template["spec"]["networkPolicy"]["ingress"], [])
        self.assertEqual(
            template["spec"]["networkPolicy"]["egress"][0]["to"],
            [{"ipBlock": {"cidr": "169.254.169.254/32"}}],
        )
        self.assertEqual(
            pool["spec"]["sandboxTemplateRef"]["name"], template["metadata"]["name"]
        )
        self.assertEqual(pool["spec"]["replicas"], 0)
        self.assertEqual(claim["spec"]["warmPoolRef"]["name"], pool["metadata"]["name"])
        for obj in (template, pool, claim):
            self.assertEqual(obj["apiVersion"], "extensions.agents.x-k8s.io/v1beta1")

    def test_rejects_unpinned_image_and_non_probe_namespace(self):
        for namespace, image in [
            ("production", "example.invalid/probe@sha256:" + "a" * 64),
            ("kit-wif-poc-test", "image:latest"),
        ]:
            with self.assertRaises(ValueError):
                manifest(
                    namespace, "probe@example-project.iam.gserviceaccount.com", image
                )


if __name__ == "__main__":
    unittest.main()
