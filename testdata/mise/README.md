# MISE YAML end-to-end fixtures

These projects verify that Bocker installs exact toolchain versions inside a
disposable builder stage, executes each project's tests, copies only build
artifacts into the final Debian 13 image, and leaves no `/opt/bocker-mise`
directory in that image.

Run each fixture with the privileged-container mode used by the production
Incusfile.yaml:

```bash
bocker image build --permission super testdata/mise/node/Incusfile.yaml
bocker image build --permission super testdata/mise/python/Incusfile.yaml
bocker image build --permission super testdata/mise/rust/Incusfile.yaml
```

The pinned toolchains are Node.js 24.19.0, Python 3.13.7, and Rust 1.89.0.
