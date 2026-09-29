from pathlib import Path
import os
import sys
import unittest
from unittest import mock

MVP_DIR = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(MVP_DIR))

import gitlab_client
from jira_client import description_text


class JiraDescriptionTest(unittest.TestCase):
    def test_server_description_is_unchanged(self):
        self.assertEqual(description_text("Обычный текст"), "Обычный текст")

    def test_cloud_adf_description_is_flattened(self):
        adf = {
            "type": "doc",
            "content": [
                {
                    "type": "paragraph",
                    "content": [
                        {"type": "text", "text": "Первая строка"},
                        {"type": "text", "text": " продолжается"},
                    ],
                },
                {
                    "type": "paragraph",
                    "content": [{"type": "text", "text": "Вторая строка"}],
                },
            ],
        }
        text = description_text(adf)
        self.assertIn("Первая строка продолжается", text)
        self.assertIn("Вторая строка", text)


class GitLabClientTest(unittest.TestCase):
    def test_api_uses_v4_prefix(self):
        response = mock.MagicMock()
        response.__enter__.return_value.read.return_value = b"{}"
        with mock.patch.dict(
            os.environ,
            {
                "GITLAB_URL": "https://gitlab.example",
                "GITLAB_TOKEN": "token",
                "HTTP_TIMEOUT_SECONDS": "5",
            },
            clear=False,
        ):
            with mock.patch.object(
                gitlab_client.urllib.request,
                "urlopen",
                return_value=response,
            ) as urlopen:
                gitlab_client.api("GET", "/user")

        request = urlopen.call_args.args[0]
        self.assertEqual(request.full_url, "https://gitlab.example/api/v4/user")
        self.assertEqual(urlopen.call_args.kwargs["timeout"], 5.0)


if __name__ == "__main__":
    unittest.main()
