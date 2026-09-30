package cli

import (
	"fmt"
	"regexp"
)

// standardKey is the naming standard, PROJECT_PLATFORM_TYPE: capitals,
// digits and underscores, starting with a letter. Cove accepts other keys
// too, but only these work as ${...} variables in a compose file, which is
// how Lighthouse hands secrets to projects.
var standardKey = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// warnNaming warns, without refusing anything, when a new key breaks the
// naming standard.
func warnNaming(key string) {
	if standardKey.MatchString(key) {
		return
	}
	warn(fmt.Sprintf("%q doesn't follow the naming standard (PROJECT_PLATFORM_TYPE, e.g. BOTSUITE_TWITCH_CLIENT_ID: "+
		"capitals, digits and _ only), so it can't be used as a ${...} name in a compose file. "+
		"Rename it with: rename %s <NEW_NAME>", key, key))
}
