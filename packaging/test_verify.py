"""Regression checks for package payload extraction."""
import io
from pathlib import Path
import tarfile
import tempfile
import unittest

import verify


def members():
    entries = []
    for name in sorted(verify.DIRECTORIES):
        entry = tarfile.TarInfo(f"./{name}" if name != "." else "./")
        entry.type = tarfile.DIRTYPE
        entry.mode = 0o755
        entries.append(entry)
    for name in sorted(verify.FILES):
        entry = tarfile.TarInfo(f"./{name}")
        entry.mode = 0o755 if name == "usr/bin/croc" else 0o644
        entry.size = len(b"payload")
        entries.append(entry)
    return entries


def payload(entries):
    data = io.BytesIO()
    with tarfile.open(fileobj=data, mode="w") as archive:
        for entry in entries:
            archive.addfile(entry, io.BytesIO(b"payload") if entry.isfile() else None)
    return data.getvalue()


class PayloadTests(unittest.TestCase):
    def reject(self, entries, message):
        with tempfile.TemporaryDirectory() as temp:
            destination = Path(temp) / "extracted"
            with self.assertRaisesRegex(RuntimeError, message):
                verify.extract_payload(payload(entries), "test package", destination)
            self.assertFalse(destination.exists())

    def test_valid_payload(self):
        with tempfile.TemporaryDirectory() as temp:
            destination = Path(temp) / "extracted"
            verify.extract_payload(payload(members()), "test package", destination)
            actual = {p.relative_to(destination).as_posix() for p in destination.rglob("*") if p.is_file()}
            self.assertEqual(actual, verify.FILES)
            for name in verify.FILES:
                self.assertEqual((destination / name).read_bytes(), b"payload")
                self.assertEqual((destination / name).stat().st_mode & 0o777,
                                 0o755 if name == "usr/bin/croc" else 0o644)

    def test_invalid_entries_are_rejected_before_extraction(self):
        for attribute, value, message in (
            ("name", "../outside", "unexpected file"),
            ("name", "/usr/bin/croc", "unexpected file"),
            ("name", "./usr/bin/other", "unexpected file"),
            ("type", tarfile.SYMTYPE, "unexpected file"),
            ("type", tarfile.LNKTYPE, "unexpected file"),
            ("uid", 1000, "non-root owner"),
            ("gid", 1000, "non-root owner"),
            ("mode", 0o777, "permissions"),
        ):
            with self.subTest(attribute=attribute, value=value):
                entries = members()
                entry = next(m for m in entries if m.name == "./usr/bin/croc")
                setattr(entry, attribute, value)
                entry.linkname = "../outside"
                self.reject(entries, message)
        for name, mode, message in (("../outside", 0o755, "unexpected directory"),
                                    ("./usr", 0o777, "directory permissions")):
            with self.subTest(directory=name, mode=mode):
                entries = members()
                entries[0].name = name
                entries[0].mode = mode
                self.reject(entries, message)

    def test_duplicate_file_is_rejected(self):
        entries = members()
        entries.append(entries[-1])
        self.reject(entries, "duplicate file")

    def test_missing_file_is_rejected(self):
        self.reject(members()[:-1], "incomplete payload")


if __name__ == "__main__":
    unittest.main()
