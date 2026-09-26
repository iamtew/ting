package master

import _ "embed"

//go:embed index.html
var indexPage []byte

//go:embed login.html
var loginPage []byte
