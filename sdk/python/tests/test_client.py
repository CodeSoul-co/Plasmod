import unittest
from unittest.mock import Mock, patch

from plasmod_sdk.client import PlasmodClient


class PlasmodClientConsistencyTest(unittest.TestCase):
    def test_get_consistency_mode_uses_admin_endpoint(self):
        response = Mock()
        response.json.return_value = {"status": "ok", "mode": "strict_visible"}

        with patch("plasmod_sdk.client.requests.get", return_value=response) as get:
            client = PlasmodClient("http://plasmod.test", timeout=3)
            result = client.get_consistency_mode()

        get.assert_called_once_with(
            "http://plasmod.test/v1/admin/consistency-mode", timeout=3
        )
        response.raise_for_status.assert_called_once_with()
        self.assertEqual(result["mode"], "strict_visible")

    def test_set_consistency_mode_posts_requested_mode(self):
        response = Mock()
        response.json.return_value = {
            "status": "ok",
            "mode": "bounded_staleness",
        }

        with patch("plasmod_sdk.client.requests.post", return_value=response) as post:
            client = PlasmodClient("http://plasmod.test", timeout=3)
            result = client.set_consistency_mode("bounded_staleness")

        post.assert_called_once_with(
            "http://plasmod.test/v1/admin/consistency-mode",
            json={"mode": "bounded_staleness"},
            timeout=3,
        )
        response.raise_for_status.assert_called_once_with()
        self.assertEqual(result["mode"], "bounded_staleness")


if __name__ == "__main__":
    unittest.main()
