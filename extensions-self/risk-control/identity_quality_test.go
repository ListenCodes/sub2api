package main

import "testing"

func TestIdentityQualityPausesCompositeWithOneInvalidDomain(t *testing.T) {
	cfg := IdentityConfig{IPCollectionEnabled: true, DeviceCollectionEnabled: true, IPDomainEnabled: true, DeviceDomainEnabled: true, CompositeDomainEnabled: true, QualityMinEvents: 10, QualityMinCoverage: 80}
	states := identityDomainStates(cfg, identityQualityCounts{Total: 10, ValidIP: 10, ValidDevice: 7})
	if states["ip"] != "healthy" || states["device"] != "paused" || states["composite"] != "paused" {
		t.Fatalf("states = %#v", states)
	}
}

func TestIdentityQualityPausesSharedNetworkDominance(t *testing.T) {
	cfg := IdentityConfig{IPCollectionEnabled: true, DeviceCollectionEnabled: true, IPDomainEnabled: true, DeviceDomainEnabled: true, CompositeDomainEnabled: true, QualityMinEvents: 10, QualityMinCoverage: 80, QualityMinUsers: 50, QualityMaxIPShare: 20}
	states := identityDomainStates(cfg, identityQualityCounts{Total: 100, ValidIP: 100, ValidDevice: 100, LinkedUsers: 100, MaxNetworkUsers: 25})
	if states["ip"] != "paused" || states["device"] != "healthy" || states["composite"] != "paused" {
		t.Fatalf("states = %#v", states)
	}
}
