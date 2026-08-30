from pathlib import Path
import json

message = Path("/etc/python-mount-message").read_text(encoding="utf-8").strip()
Path("/var/lib/python-mount-app/result.txt").write_text("python-mounted\n", encoding="utf-8")
print(json.dumps({"language": "python", "message": message}))
