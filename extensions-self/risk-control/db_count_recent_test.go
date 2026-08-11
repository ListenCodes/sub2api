package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCountRecentQueryUsesContiguousParameters(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name     string
		strategy string
		wantArgs int
	}{
		{name: "associated events", strategy: countStrategyAssociatedEvents, wantArgs: 6},
		{name: "subject or device", strategy: countStrategySubjectDeviceEvents, wantArgs: 4},
		{name: "distinct subjects by IP", strategy: countStrategyIPDistinctSubjects, wantArgs: 4},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query, args := countRecentQuery(7, "subject", "ip", "device", "registration_success", test.strategy, now)
			if len(args) != test.wantArgs {
				t.Fatalf("argument count = %d, want %d", len(args), test.wantArgs)
			}
			for index := 1; index <= len(args); index++ {
				if !strings.Contains(query, fmt.Sprintf("$%d", index)) {
					t.Fatalf("query is missing bound parameter $%d: %s", index, query)
				}
			}
			if strings.Contains(query, fmt.Sprintf("$%d", len(args)+1)) {
				t.Fatalf("query references unbound parameter $%d: %s", len(args)+1, query)
			}
		})
	}
}
