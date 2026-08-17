# ASDF Incusfile end-to-end fixtures

These projects verify that Bocker installs exact toolchain versions inside a
disposable `TEMP` stage, executes each project's tests, copies only build
artifacts into the final Debian 13 image, and leaves no `/opt/bocker-asdf`
directory in that image.

Run each fixture with the privileged-container mode used by the production
Incusfile:

```bash
bocker image build --permission super testdata/asdf/node/Incusfile
bocker image build --permission super testdata/asdf/python/Incusfile
bocker image build --permission super testdata/asdf/rust/Incusfile
```

The pinned toolchains are Node.js 24.19.0, Python 3.13.7, and Rust 1.89.0.
