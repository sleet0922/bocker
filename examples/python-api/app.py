import os
import tomllib
from pathlib import Path

from flask import Flask, jsonify


def load_config():
    path = Path(os.environ.get("APP_CONFIG", "/etc/python-api/config.toml"))
    with path.open("rb") as handle:
        config = tomllib.load(handle)
    service = config.setdefault("service", {})
    if os.environ.get("APP_MESSAGE"):
        service["message"] = os.environ["APP_MESSAGE"]
    if os.environ.get("APP_VERSION"):
        service["version"] = os.environ["APP_VERSION"]
    return config


config = load_config()
app = Flask(__name__)


@app.get("/healthz")
def healthz():
    return jsonify(status="ok", language="python")


@app.get("/config")
def show_config():
    return jsonify(config)


@app.get("/")
def index():
    service = config["service"]
    return f"{service['message']} ({service['version']})\n"
