package checks

import "strings"

// ServiceCheckType maps service kinds to the probe protocol used by generic checks.
func ServiceCheckType(kind string) string {
	switch strings.ToUpper(strings.TrimSpace(kind)) {
	case "CUSTOM_FTP", "FTP":
		return "FTP"
	default:
		return "HTTP"
	}
}
