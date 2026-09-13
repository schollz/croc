"""Regression checks for publication boundaries and command installation."""
import contextlib
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest

import release


class ReleaseTests(unittest.TestCase):
    def test_exact_assets_and_corruption(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp)
            names = release.expected_assets("11.5.2")
            self.assertEqual(len(names), 34)
            self.assertEqual(sum(n.endswith(".deb") for n in names), 6)
            self.assertEqual(sum(n.endswith(".rpm") for n in names), 6)
            for name in names - {"croc_v11.5.2_checksums.txt"}:
                (path / name).write_bytes(b"test asset")
            with contextlib.redirect_stdout(io.StringIO()):
                release.check_assets(path, "11.5.2", write=True)
                release.check_assets(path, "11.5.2")
            (path / "croc_11.5.2-1_armel.deb").write_bytes(b"corrupt")
            with self.assertRaisesRegex(ValueError, "checksums"):
                release.check_assets(path, "11.5.2")
            (path / "croc_11.5.2-1_armel.deb").unlink()
            with self.assertRaisesRegex(ValueError, "missing=.*armel"):
                release.check_assets(path, "11.5.2", write=True)

    def test_unexpected_assets_block_publication(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp)
            for name in release.expected_assets("11.5.2"):
                (path / name).touch()
            (path / "croc_11.5.2-1_armv5.deb").touch()
            with self.assertRaisesRegex(ValueError, "unexpected=.*armv5"):
                release.check_assets(path, "11.5.2", write=True)

    def test_remote_release_must_be_draft_with_exact_assets(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / "release.json"
            names = sorted(release.expected_assets("11.5.2"))
            metadata = {"isDraft": True, "assets": [{"name": name} for name in names]}
            path.write_text(json.dumps(metadata))
            release.check_draft(path, "11.5.2")
            metadata["isDraft"] = False
            path.write_text(json.dumps(metadata))
            with self.assertRaises(ValueError):
                release.check_draft(path, "11.5.2")
            metadata["isDraft"] = True
            metadata["assets"][-1] = metadata["assets"][0]
            path.write_text(json.dumps(metadata))
            with self.assertRaises(ValueError):
                release.check_draft(path, "11.5.2")

    def test_archive_must_contain_one_regular_binary(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp)
            for name in ("../croc", "croc"):
                with tarfile.open(path / "archive.tar", "w") as archive:
                    entry = tarfile.TarInfo(name)
                    entry.type = tarfile.SYMTYPE
                    entry.linkname = "/usr/bin/croc"
                    archive.addfile(entry)
                with self.assertRaisesRegex(ValueError, "regular croc"):
                    release.unpack_binary(path / "archive.tar", path / "binary")

    def test_stable_versions_only(self):
        for bad in ("v01.2.3", "11.5.2-rc1", "11.5", "../11.5.2"):
            with self.assertRaises(ValueError):
                release.version(bad)


if __name__ == "__main__":
    unittest.main()
