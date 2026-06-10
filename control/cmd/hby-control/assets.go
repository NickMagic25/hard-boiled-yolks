package main

import (
	_ "embed"
)

//go:embed web/html/index.html
var indexHTML string

//go:embed web/html/login.html
var loginHTML string

//go:embed web/js/index.js
var indexJS string

//go:embed web/js/login.js
var loginJS string

//go:embed web/js/theme.js
var themeJS string
