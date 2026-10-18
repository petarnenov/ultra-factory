package daemon

import "testing"

func TestReportSourceHostNamePrefersConfiguredNames(t *testing.T) {
	t.Setenv("AO_HOST_NAME", "  Hub Display Name ")
	t.Setenv("RAILWAY_SERVICE_NAME", "railway-hub")
	if got := reportSourceHostName(); got != "Hub Display Name" {
		t.Fatalf("reportSourceHostName() = %q", got)
	}
}

func TestReportSourceHostNameUsesRailwayServiceNameBeforeContainerID(t *testing.T) {
	if got := resolveReportSourceHostName(" ", "ao-hub", "9f2a2dbe7a129cff", nil); got != "ao-hub" {
		t.Fatalf("reportSourceHostName() = %q", got)
	}
}

func TestReportSourceHostNameRejectsContainerIDFallback(t *testing.T) {
	if got := resolveReportSourceHostName("", "", "9f2a2dbe7a129cff", nil); got != "" {
		t.Fatalf("reportSourceHostName() = %q, want empty for a container ID", got)
	}
}
