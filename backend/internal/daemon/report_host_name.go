package daemon

import (
	"os"
	"regexp"
	"strings"
)

var containerHostID = regexp.MustCompile(`^[0-9a-fA-F]{12,64}$`)

func reportSourceHostName() string {
	hostname, err := os.Hostname()
	return resolveReportSourceHostName(os.Getenv("AO_HOST_NAME"), os.Getenv("RAILWAY_SERVICE_NAME"), hostname, err)
}

func resolveReportSourceHostName(configured, railwayService, hostname string, err error) string {
	if name := strings.TrimSpace(configured); name != "" {
		return name
	}
	if name := strings.TrimSpace(railwayService); name != "" {
		return name
	}
	if err != nil || containerHostID.MatchString(hostname) {
		return ""
	}
	return strings.TrimSpace(hostname)
}
