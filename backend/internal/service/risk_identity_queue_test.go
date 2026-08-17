package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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

func TestRiskIdentityHeartbeatReportsDeliveryWatermarkAndFailures(t *testing.T) {
	var received RiskIdentityDeliveryReport
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/internal/identity-delivery" || r.Header.Get("X-Risk-Signature") == "" {
			t.Fatalf("unexpected signed heartbeat request: %s signature=%q", r.URL.Path, r.Header.Get("X-Risk-Signature"))
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, &received); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	startedAt := time.Now().UTC().Add(-time.Second)
	client := &RiskControlClient{baseURL: server.URL, secret: []byte("test-risk-secret"), http: server.Client(), identityEnabled: true, identityDeliveryEnabled: true, identitySource: "sub2api-test", identityGeneration: "generation-test", identityStartedAt: startedAt, identityQueue: make(chan RiskIdentityReport, 3)}
	client.identityEnqueued.Store(5)
	client.identitySucceeded.Store(2)
	client.identityFailed.Store(1)
	client.identityDropped.Store(2)
	client.identityQueue <- RiskIdentityReport{EventKey: "pending"}
	if err := client.sendIdentityHeartbeat(context.Background()); err != nil {
		t.Fatalf("sendIdentityHeartbeat() error = %v", err)
	}
	if received.Source != "sub2api-test" || received.Generation != "generation-test" || received.StartedAt != startedAt.Format(time.RFC3339Nano) || received.Sequence != 1 || received.Enqueued != 5 || received.Succeeded != 2 || received.Failed != 1 || received.Dropped != 2 || received.QueueDepth != 1 || received.GeneratedAt == "" {
		t.Fatalf("heartbeat = %+v", received)
	}
	if !client.identityDeliveryReady.Load() {
		t.Fatal("successful heartbeat did not establish the delivery watermark")
	}
}

func TestRiskIdentityWorkerEstablishesDeliveryWatermarkBeforeFirstEvent(t *testing.T) {
	paths := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := &RiskControlClient{baseURL: server.URL, secret: []byte("test-risk-secret"), http: server.Client(), identityEnabled: true, identityDeliveryEnabled: true, identitySource: "sub2api-test", identityQueue: make(chan RiskIdentityReport, 1)}
	client.identityQueue <- RiskIdentityReport{EventKey: "watermarked-event", EventType: "registration_success"}
	close(client.identityQueue)
	client.runIdentityWorker()
	if len(paths) != 2 || paths[0] != "/api/v1/internal/identity-delivery" || paths[1] != "/api/v1/internal/identity-events" {
		t.Fatalf("delivery request order = %#v", paths)
	}
	if client.identitySucceeded.Load() != 1 || client.identityFailed.Load() != 0 {
		t.Fatalf("succeeded=%d failed=%d", client.identitySucceeded.Load(), client.identityFailed.Load())
	}
}

func TestRiskIdentityWorkerRetriesWithStableEventKey(t *testing.T) {
	attempts := 0
	keys := make([]string, 0, riskIdentityAttempts)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		var report RiskIdentityReport
		if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, report.EventKey)
		if attempts < riskIdentityAttempts {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := &RiskControlClient{baseURL: server.URL, secret: []byte("test-risk-secret"), http: server.Client(), identityEnabled: true, identityQueue: make(chan RiskIdentityReport, 1)}
	client.identityQueue <- RiskIdentityReport{EventKey: "registration-identity-stable", EventType: "registration_success"}
	close(client.identityQueue)
	client.runIdentityWorker()
	if attempts != riskIdentityAttempts || client.identitySucceeded.Load() != 1 || client.identityFailed.Load() != 0 {
		t.Fatalf("attempts=%d succeeded=%d failed=%d", attempts, client.identitySucceeded.Load(), client.identityFailed.Load())
	}
	for _, key := range keys {
		if key != "registration-identity-stable" {
			t.Fatalf("retry keys = %#v", keys)
		}
	}
}
