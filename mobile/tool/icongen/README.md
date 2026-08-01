# Launcher icon generator

Regenerates `android/app/src/main/res/mipmap-*/ic_launcher*.png`.

```bash
go run ./tool/icongen ./android/app/src/main/res
```

The mark is the same reactor core the desktop draws in its tray: a ring and a
filled centre, in `live` on `deck-900`. It is generated rather than exported
from a drawing so that the densities cannot drift apart from one another, and
so that a palette change is a one-line edit instead of ten re-exports. Nothing
but the Go standard library is involved.

Two sets come out of it. The `ic_launcher` PNGs are the square legacy icon.
The `ic_launcher_foreground` PNGs are the adaptive layer, drawn smaller on a
transparent field because launchers crop adaptive icons to their own shape and
only the middle two thirds is guaranteed to survive.
