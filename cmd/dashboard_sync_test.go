package cmd

import (
	"testing"

	"github.com/wirelogai/wirelog-cli/internal/dashboard"
)

func TestDashboardSyncIdentifier(t *testing.T) {
	tests := []struct {
		dashboard *dashboard.Dashboard
		path      string
		override  string
		want      string
	}{
		{dashboard: &dashboard.Dashboard{ID: "product-growth"}, path: "ignored.yaml", want: "product-growth"},
		{dashboard: &dashboard.Dashboard{}, path: "Focus Dashboard.yaml", want: "focus-dashboard"},
		{dashboard: &dashboard.Dashboard{}, path: "legacy.yml", override: "stable-id", want: "stable-id"},
	}
	for _, test := range tests {
		got, err := dashboardSyncIdentifier(test.dashboard, test.path, test.override)
		if err != nil {
			t.Fatalf("dashboardSyncIdentifier(%q): %v", test.path, err)
		}
		if got != test.want {
			t.Errorf("dashboardSyncIdentifier(%q) = %q, want %q", test.path, got, test.want)
		}
	}
}

func TestDashboardSyncIdentifierRejectsInvalidID(t *testing.T) {
	_, err := dashboardSyncIdentifier(&dashboard.Dashboard{ID: "Not Valid"}, "dashboard.yaml", "")
	if err == nil {
		t.Fatal("expected invalid dashboard id error")
	}
}
