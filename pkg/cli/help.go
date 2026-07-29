package cli

import "strings"

const appNamePlaceholder = "{{app}}"

// RenderAppText substitutes the configured binary name in shared command help.
// Customer commands are assembled into both hyperslop and the admin datadrop
// binary, so hard-coding either executable makes the other one's copy/paste
// examples wrong.
func RenderAppText(text string) string {
	return strings.ReplaceAll(text, appNamePlaceholder, AppName())
}
