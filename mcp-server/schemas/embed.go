// Package schemas carries BrowserNERD's built-in Mangle schema inside the
// binary. The server reasons over the fact model its own Go code emits, so the
// schema that declares those facts ships with that code: a workspace can add
// project modules on top, but it can never run against a stale copy of the
// built-in declarations.
package schemas

import "embed"

// FS holds the built-in manifest (browser.mg) and every module it includes.
//
//go:embed browser.mg modules/*.mg
var FS embed.FS

// Manifest is the path of the built-in manifest inside FS.
const Manifest = "browser.mg"
