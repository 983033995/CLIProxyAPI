import importlib.util
import json
import pathlib
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("registry_to_cpa", ROOT / "tools" / "registry_to_cpa.py")
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

class RegistryConversionTests(unittest.TestCase):
    def setUp(self):
        self.registry = json.loads((ROOT / "tests" / "fixture_registry.json").read_text())

    def test_only_eligible_chat_apikey_with_https_base(self):
        generated, report = module.convert(self.registry)
        self.assertEqual(report["selectedCount"], 1)
        self.assertEqual(report["skippedCount"], 5)
        self.assertEqual(report["modelCount"], 1)
        self.assertIn('prefix: "ofm-good"', generated)
        self.assertIn('name: "org/model"', generated)
        self.assertIn("input-modalities: [text, image]", generated)
        self.assertIn("disabled: true", generated)
        self.assertIn("keys: []", generated)
        self.assertNotIn("Bearer", generated)

    def test_auth_and_wire_are_distinct_dimensions(self):
        _, report = module.convert(self.registry)
        reasons = {x["id"]: x["reason"] for x in report["skipped"]}
        self.assertEqual(reasons["anthropic"], "non-chat-format")
        self.assertEqual(reasons["vertex"], "non-apikey")
        self.assertEqual(reasons["azure_openai"], "missing-or-unsafe-base-url")
        self.assertEqual(reasons["keyless"], "non-apikey")
        self.assertEqual(reasons["workbuddy"], "upstream-excluded")

    def test_insecure_urls_rejected(self):
        for url in ("http://api.example.org/v1", "https://user:pass@example.org/v1",
                    "https://example.org/$TOKEN", "https://example.org/<PLACEHOLDER>"):
            self.assertFalse(module.allowed_url(url))

    def test_duplicate_provider_id_rejected(self):
        with self.assertRaises(ValueError):
            module.convert(self.registry + [self.registry[0]])

if __name__ == "__main__":
    unittest.main()
