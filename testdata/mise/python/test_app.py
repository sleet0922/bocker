import bz2
import lzma
import sqlite3
import ssl
import unittest

from app import build_result


class AppTest(unittest.TestCase):
    def test_build_result_reports_requested_runtime(self) -> None:
        self.assertEqual(
            build_result(),
            {"language": "python", "version": "3.13.7", "checksum": 28},
        )

    def test_standard_library_extensions_are_available(self) -> None:
        self.assertTrue(ssl.OPENSSL_VERSION)
        self.assertEqual(sqlite3.connect(":memory:").execute("select 28").fetchone(), (28,))
        self.assertEqual(bz2.decompress(bz2.compress(b"ok")), b"ok")
        self.assertEqual(lzma.decompress(lzma.compress(b"ok")), b"ok")


if __name__ == "__main__":
    unittest.main()
