"use strict";

const assert = require("node:assert/strict");
const test = require("node:test");
const { buildResult } = require("./app");

test("buildResult reports the requested Node runtime", () => {
  assert.deepEqual(buildResult(), {
    language: "node",
    version: "24.19.0",
    checksum: 28,
  });
});
