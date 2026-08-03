# Node.js API

This example installs Node.js and Express through the npmmirror registry in a
separate Alpine build stage, then copies only production dependencies and app
files into the runtime image. OpenRC manages the process and handles stop and
restart signals cleanly.

Endpoints: `/healthz`, `/config`, and `/`.
