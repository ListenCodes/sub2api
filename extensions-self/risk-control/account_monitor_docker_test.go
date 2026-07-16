package main

import (
	"os"
	"strings"
	"testing"
)

func TestDockerfileBuildsAccountMonitorIntoSingleBinary(t *testing.T) {
	raw, err := os.ReadFile("../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
	for _, required := range []string{
		"COPY account-monitor/go.mod account-monitor/go.sum",
		"COPY account-monitor/",
		"-o /out/extensions-self",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("Dockerfile missing %q", required)
		}
	}
	for _, forbidden := range []string{"account-monitor/web", "EXTENSIONS_SELF_ACCOUNT_MONITOR_WEB_DIR"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("Dockerfile still contains obsolete static monitor wiring %q", forbidden)
		}
	}
}
