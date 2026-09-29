import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

MVP_DIR = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(MVP_DIR))

import publish


class PublishHelpersTest(unittest.TestCase):
    def test_issue_key_is_normalized(self):
        self.assertEqual(publish.validate_issue_key("intaidev-101"), "INTAIDEV-101")

    def test_invalid_issue_key_is_rejected(self):
        with self.assertRaises(SystemExit):
            publish.validate_issue_key("../../main")

    def test_remote_project_supports_ssh_and_https(self):
        cases = {
            "git@gitlab.example:rd/1c-ai/demo.git": ("ssh", "gitlab.example", "rd/1c-ai/demo"),
            "https://gitlab.example:8443/rd/demo.git": ("https", "gitlab.example:8443", "rd/demo"),
            "ssh://git@gitlab.example/rd/demo.git": ("ssh", "gitlab.example", "rd/demo"),
        }
        for origin, expected in cases.items():
            with self.subTest(origin=origin), mock.patch.object(publish, "git", return_value=origin):
                self.assertEqual(publish.remote_project("/repo"), expected)

    def test_gitlab_host_must_match_origin(self):
        with mock.patch.dict(os.environ, {"GITLAB_URL": "https://gitlab.example"}, clear=False):
            publish.assert_gitlab_host("gitlab.example")
            with self.assertRaises(SystemExit):
                publish.assert_gitlab_host("evil.example")

    def test_redact_hides_known_tokens_and_url_credentials(self):
        with mock.patch.dict(
            os.environ,
            {"GITLAB_TOKEN": "secret-token", "JIRA_TOKEN": "jira-secret"},
            clear=False,
        ):
            text = "https://oauth2:secret-token@gitlab.example secret-token jira-secret"
            safe = publish.redact(text)
        self.assertNotIn("secret-token", safe)
        self.assertNotIn("jira-secret", safe)
        self.assertIn("https://***@gitlab.example", safe)

    def test_https_push_uses_temporary_askpass(self):
        observed = {}

        def fake_git(_repo, *args, env=None):
            observed["args"] = args
            observed["env"] = env
            askpass = Path(env["GIT_ASKPASS"])
            observed["script"] = askpass.read_text(encoding="utf-8")
            self.assertTrue(askpass.is_file())
            return ""

        with mock.patch.dict(os.environ, {"GITLAB_TOKEN": "secret-token"}, clear=False):
            with mock.patch.object(publish, "git", side_effect=fake_git):
                publish.push_branch("/repo", "agent/INTAIDEV-1", "https")

        self.assertNotIn("secret-token", " ".join(observed["args"]))
        self.assertNotIn("secret-token", observed["script"])
        self.assertEqual(observed["env"]["GIT_TERMINAL_PROMPT"], "0")


class PrepareWorkflowTest(unittest.TestCase):
    def git(self, *args, cwd=None):
        return subprocess.run(
            ["git", *args],
            cwd=cwd,
            check=True,
            capture_output=True,
            text=True,
        ).stdout.strip()

    def test_prepare_creates_issue_branch_from_origin_main(self):
        with tempfile.TemporaryDirectory(prefix="1c-vibe-test-") as temp_dir:
            root = Path(temp_dir)
            remote = root / "remote.git"
            seed = root / "seed"
            work = root / "work"

            self.git("init", "--bare", str(remote))
            self.git("init", "--initial-branch=main", str(seed))
            self.git("config", "user.name", "1c-vibe test", cwd=seed)
            self.git("config", "user.email", "test@example.invalid", cwd=seed)
            (seed / "README.md").write_text("demo\n", encoding="utf-8")
            self.git("add", "README.md", cwd=seed)
            self.git("commit", "-m", "initial", cwd=seed)
            self.git("remote", "add", "origin", str(remote), cwd=seed)
            self.git("push", "-u", "origin", "main", cwd=seed)
            self.git("symbolic-ref", "HEAD", "refs/heads/main", cwd=remote)
            self.git("clone", str(remote), str(work))

            with mock.patch.object(
                publish,
                "project_and_base",
                return_value=({"id": 1}, "main", "rd/demo"),
            ):
                publish.prepare(str(work), "INTAIDEV-42", None)

            branch = self.git("branch", "--show-current", cwd=work)
            self.assertEqual(branch, "agent/INTAIDEV-42")
            self.assertEqual(self.git("status", "--porcelain", cwd=work), "")


if __name__ == "__main__":
    unittest.main()
