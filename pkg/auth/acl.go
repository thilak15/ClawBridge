package auth

import (
	"path"
	"strings"
)

// IsAuthorized checks if any of the agent's allowed rules match the target channel and action.
func IsAuthorized(rules []string, channel, action string) bool {
	target := strings.ToLower(fmtPattern(channel, action))

	for _, rule := range rules {
		cleanRule := strings.ToLower(strings.TrimSpace(rule))
		if cleanRule == "*" || cleanRule == target {
			return true
		}

		// Channel wildcard matching (e.g. "slack:*")
		if strings.HasSuffix(cleanRule, ":*") {
			prefix := strings.TrimSuffix(cleanRule, "*")
			if strings.HasPrefix(target, prefix) {
				return true
			}
		}

		// Pattern matching (e.g. "*:read*", "jira:*")
		matched, _ := path.Match(cleanRule, target)
		if matched {
			return true
		}
	}

	return false
}

func fmtPattern(channel, action string) string {
	return channel + ":" + action
}

