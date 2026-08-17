"use strict";

function buildResult() {
  return {
    language: "node",
    version: process.versions.node,
    checksum: [2, 3, 5, 7, 11].reduce((sum, value) => sum + value, 0),
  };
}

if (require.main === module) {
  process.stdout.write(`${JSON.stringify(buildResult())}\n`);
}

module.exports = { buildResult };
