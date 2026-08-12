package service

import "testing"

func TestRiskIdentityQueueFullDropsWithoutBlocking(t *testing.T) {
	client := &RiskControlClient{identityEnabled: true, identityQueue: make(chan RiskIdentityReport, 1)}
	if !client.EnqueueIdentity(RiskIdentityReport{EventKey: "one"}) {
		t.Fatal("first enqueue failed")
	}
	if client.EnqueueIdentity(RiskIdentityReport{EventKey: "two"}) {
		t.Fatal("full queue accepted event")
	}
	if client.IdentityDropped() != 1 {
		t.Fatalf("dropped = %d", client.IdentityDropped())
	}
	health := client.IdentityQueueHealth()
	if health["enqueued"] != uint64(1) || health["dropped"] != uint64(1) || health["state"] != "degraded" {
		t.Fatalf("queue health = %#v", health)
	}
}

func TestRiskIdentityCollectionSwitchesAreIndependent(t *testing.T) {
	t.Setenv("RISK_CONTROL_URL", "http://identity.invalid")
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "01234567890123456789012345678901")
	t.Setenv("RISK_IDENTITY_V2_ENABLED", "true")
	t.Setenv("RISK_IDENTITY_IP_COLLECTION_ENABLED", "true")
	t.Setenv("RISK_IDENTITY_DEVICE_COLLECTION_ENABLED", "false")
	client := NewRiskControlClientFromEnv()
	if !client.IdentityIPEnabled() || client.IdentityDeviceEnabled() {
		t.Fatalf("collection switches = ip %v device %v", client.IdentityIPEnabled(), client.IdentityDeviceEnabled())
	}
}
