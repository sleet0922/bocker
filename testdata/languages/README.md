# Language Incusfile fixtures

The `go`, `node`, and `python` projects are small buildable examples for the
Incusfile v2 runtime mount syntax. Their Incusfiles mount a relative host
directory read/write and a relative host file read-only when the image is run.

Run the real-daemon checks with:

```bash
BOCKER_BIN=./bocker testdata/languages/test.sh
```
