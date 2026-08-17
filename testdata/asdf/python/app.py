import json
import platform


def build_result() -> dict[str, object]:
    return {
        "language": "python",
        "version": platform.python_version(),
        "checksum": sum((2, 3, 5, 7, 11)),
    }


if __name__ == "__main__":
    print(json.dumps(build_result(), sort_keys=True))
