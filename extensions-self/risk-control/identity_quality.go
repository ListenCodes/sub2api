package main

type identityQualityCounts struct{ Total, ValidIP, ValidDevice, LinkedUsers, MaxNetworkUsers int64 }

func identityDomainStates(cfg IdentityConfig, counts identityQualityCounts) map[string]string {
	states := map[string]string{"ip": "disabled", "device": "disabled", "composite": "disabled"}
	if cfg.IPCollectionEnabled && cfg.IPDomainEnabled {
		states["ip"] = identityQualityState(counts.Total, counts.ValidIP, cfg)
		minimumUsers := cfg.QualityMinUsers
		if minimumUsers <= 0 {
			minimumUsers = 50
		}
		maxShare := cfg.QualityMaxIPShare
		if maxShare <= 0 || maxShare > 100 {
			maxShare = 20
		}
		if counts.LinkedUsers >= minimumUsers && counts.MaxNetworkUsers*100 > counts.LinkedUsers*maxShare {
			states["ip"] = "paused"
		}
	}
	if cfg.DeviceCollectionEnabled && cfg.DeviceDomainEnabled {
		states["device"] = identityQualityState(counts.Total, counts.ValidDevice, cfg)
	}
	if cfg.CompositeDomainEnabled && cfg.IPCollectionEnabled && cfg.DeviceCollectionEnabled && cfg.IPDomainEnabled && cfg.DeviceDomainEnabled {
		if states["ip"] == "healthy" && states["device"] == "healthy" {
			states["composite"] = "healthy"
		} else {
			states["composite"] = "paused"
		}
	}
	return states
}

func identityQualityState(total, valid int64, cfg IdentityConfig) string {
	minimum := cfg.QualityMinEvents
	if minimum <= 0 {
		minimum = 50
	}
	coverage := cfg.QualityMinCoverage
	if coverage <= 0 || coverage > 100 {
		coverage = 80
	}
	if total < minimum {
		return "degraded"
	}
	if valid*100 < total*coverage {
		return "paused"
	}
	return "healthy"
}
