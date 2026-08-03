# Python API

This example uses Flask and Gunicorn from the Tsinghua PyPI mirror, keeps the
virtual environment in a separate build stage, and runs two workers under
OpenRC. Configuration comes from TOML with environment overrides.

Endpoints: `/healthz`, `/config`, and `/`.
