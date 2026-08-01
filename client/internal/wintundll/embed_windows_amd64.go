//go:build windows && amd64

package wintundll

import _ "embed"

//go:embed bin/wintun-amd64.dll
var driver []byte
