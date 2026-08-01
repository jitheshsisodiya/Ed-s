//go:build windows && arm64

package wintundll

import _ "embed"

//go:embed bin/wintun-arm64.dll
var driver []byte
