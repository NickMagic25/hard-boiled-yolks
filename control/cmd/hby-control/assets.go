package main

import (
	_ "embed"
)

//go:embed web/index.html
var indexHTML string

//go:embed web/login.html
var loginHTML string

//go:embed web/index.js
var indexJS string

//go:embed web/theme.js
var themeJS string
