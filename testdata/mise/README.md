# MISE YAML end-to-end fixtures

These projects verify that Bocker installs exact toolchain versions inside a
disposable builder stage, executes each project's tests, copies only build
artifacts into the final Debian 13 image, and leaves no `/opt/bocker-mise`
directory in that image.

Run each fixture with the same container policy used by production builds:

```bash
bocker image build testdata/mise/node/Incusfile
bocker image build testdata/mise/python/Incusfile
bocker image build testdata/mise/rust/Incusfile
```

The pinned toolchains are Node.js 24.19.0, Python 3.13.7, and Rust 1.89.0.
