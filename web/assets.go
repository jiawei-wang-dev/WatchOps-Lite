package webui

import "embed"

// Files contains the build-free local demo console assets.
//
//go:embed index.html app.js i18n.js styles.css
var Files embed.FS
