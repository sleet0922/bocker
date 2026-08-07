# Bocker GUI

Ubuntu Linux desktop application for Bocker, built with Flutter and Material 3.
It is a separate frontend: it invokes the existing `bocker` command and does
not change the CLI or its state format.

## Run in development

From this directory:

```bash
flutter run -d linux
```

The application uses `/usr/local/bin/bocker`. It requests authorization once,
then starts a root-owned local helper for the current desktop session. Later
GUI operations use that helper, so they do not prompt again. Rebuild and install
Bocker before using a release GUI with this source version.

For an uninstalled development binary, set its path:

```bash
BOCKER_BINARY="$PWD/../bocker" flutter run -d linux
```

The desktop session needs a PolicyKit authentication agent, which is included
by default on standard Ubuntu desktop installations.

## Build a release

```bash
./build_release.sh
```

The GUI executable is written to
`build/linux/x64/release/bundle/bocker_gui`. Install the matching CLI separately:

```bash
sudo install -m 0755 ../bocker /usr/local/bin/bocker
```
