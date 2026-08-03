# Go API

This example builds a statically linked Go HTTP service in an Alpine builder,
uses `goproxy.cn` and the Aliyun Alpine mirror, and copies only the binary and
runtime configuration into the final image. OpenRC starts it automatically.

Endpoints: `/healthz`, `/config`, and `/`. Override `APP_MESSAGE`,
`APP_VERSION`, or `APP_TIMEOUT_MS` through the `ENV` layer or `bocker exec`.
