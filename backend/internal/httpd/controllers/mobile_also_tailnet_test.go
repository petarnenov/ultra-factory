package controllers

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/mobilebridge"
)

type fixedTailnetFake struct {
	fakeLAN
	startErr  error
	requested int
	onStart   func()
}

type legacyPortFake struct {
	fakeLAN
	bound int
}

func (f *legacyPortFake) BoundPort() int { return f.bound }

func (f *fixedTailnetFake) StartTailnetFixed(port int, host string) (int, error) {
	f.requested = port
	if f.startErr != nil {
		return 0, f.startErr
	}
	if f.onStart != nil {
		f.onStart()
	}
	f.running = true
	f.bindHost = host
	return port, nil
}

func (f *fixedTailnetFake) BoundHost() string { return f.bindHost }

func TestEnableAlsoTailnetPreservesLANAndAdvertisesFixedPort(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mobile", "config.json")
	state := mobilebridge.State{Enabled: true, Password: "mobile-pass", LastPort: 3200}
	if err := mobilebridge.Save(path, state); err != nil {
		t.Fatal(err)
	}
	lan := &legacyPortFake{fakeLAN: fakeLAN{running: true, hash: "mobile-hash", bindHost: "0.0.0.0"}, bound: 57451}
	tailnet := &fixedTailnetFake{}
	b := &BridgeService{
		LAN: lan, TailnetLAN: tailnet, ConfigPath: path, TailnetPort: 3011,
		PickLANHosts:       func() []string { return []string{"192.168.1.10"} },
		PickTailscaleHosts: func() []string { return []string{"100.64.0.10"} },
	}
	status, err := b.EnableAlsoTailnet()
	if err != nil {
		t.Fatal(err)
	}
	if !lan.Running() || lan.bindHost != "0.0.0.0" || lan.PasswordHash() != "mobile-hash" {
		t.Fatalf("LAN listener changed: running=%v bind=%q hash=%q", lan.Running(), lan.bindHost, lan.PasswordHash())
	}
	if !lan.identityOnly {
		t.Fatal("tailnet password fallback remained enabled on the legacy listener")
	}
	if !status.TailnetListenerEnabled || status.TailnetListenerPort != 3011 || tailnet.requested != 3011 {
		t.Fatalf("tailnet status=%+v requested=%d", status, tailnet.requested)
	}
	if status.Port != 57451 || len(status.Endpoints) != 2 || status.Endpoints[0].Port != 57451 || status.Endpoints[1].Host != "100.64.0.10" || status.Endpoints[1].Port != 3011 {
		t.Fatalf("endpoints=%+v", status.Endpoints)
	}
	saved, err := mobilebridge.Load(path)
	if err != nil || !saved.AlsoTailnet || !saved.Enabled || saved.Password != "mobile-pass" {
		t.Fatalf("saved state=%+v err=%v", saved, err)
	}
}

func TestEnableAlsoTailnetReusesLegacyListenerOnDiscoveryPort(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mobile", "config.json")
	if err := mobilebridge.Save(path, mobilebridge.State{Enabled: true, Password: "mobile-pass"}); err != nil {
		t.Fatal(err)
	}
	lan := &fakeLAN{running: true, bindHost: "0.0.0.0"}
	b := &BridgeService{
		LAN: lan, ConfigPath: path,
		PickLANHosts:       func() []string { return []string{"192.168.1.10"} },
		PickTailscaleHosts: func() []string { return []string{"100.64.0.10"} },
	}
	status, err := b.EnableAlsoTailnet()
	if err != nil {
		t.Fatal(err)
	}
	if !status.TailnetListenerEnabled || status.TailnetListenerPort != mobilebridge.DefaultPort || !lan.identityOnly {
		t.Fatalf("tailnet listener status=%+v identityOnly=%v", status, lan.identityOnly)
	}
	if len(status.Endpoints) != 2 || status.Endpoints[1].Kind != mobilebridge.KindTailscale || status.Endpoints[1].Port != mobilebridge.DefaultPort {
		t.Fatalf("endpoints=%+v", status.Endpoints)
	}
}

func TestEnableAlsoTailnetFailureLeavesLegacyListenerUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mobile", "config.json")
	if err := mobilebridge.Save(path, mobilebridge.State{Enabled: true, Password: "mobile-pass"}); err != nil {
		t.Fatal(err)
	}
	lan := &legacyPortFake{fakeLAN: fakeLAN{running: true, hash: "mobile-hash", bindHost: "0.0.0.0"}, bound: 57451}
	tailnet := &fixedTailnetFake{startErr: errors.New("address already in use")}
	b := &BridgeService{
		LAN: lan, TailnetLAN: tailnet, ConfigPath: path,
		PickTailscaleHosts: func() []string { return []string{"100.64.0.10"} },
	}
	if _, err := b.EnableAlsoTailnet(); err == nil {
		t.Fatal("expected fixed-port bind failure")
	}
	if !lan.Running() || lan.bindHost != "0.0.0.0" || tailnet.Running() {
		t.Fatalf("partial enable changed listener state: LAN=%+v tailnet=%+v", lan, tailnet)
	}
	saved, err := mobilebridge.Load(path)
	if err != nil || saved.AlsoTailnet {
		t.Fatalf("failed enable changed persisted state: %+v err=%v", saved, err)
	}
}

func TestEnableAlsoTailnetPersistenceFailureStopsPartialListener(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mobile", "config.json")
	if err := mobilebridge.Save(path, mobilebridge.State{Enabled: true, Password: "mobile-pass"}); err != nil {
		t.Fatal(err)
	}
	lan := &legacyPortFake{fakeLAN: fakeLAN{running: true, bindHost: "0.0.0.0"}, bound: 57451}
	tailnet := &fixedTailnetFake{}
	tailnet.onStart = func() {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	b := &BridgeService{
		LAN: lan, TailnetLAN: tailnet, ConfigPath: path,
		PickTailscaleHosts: func() []string { return []string{"100.64.0.10"} },
	}
	if _, err := b.EnableAlsoTailnet(); err == nil {
		t.Fatal("expected state persistence failure")
	}
	if tailnet.Running() || tailnet.stopCalls != 1 || !lan.Running() {
		t.Fatalf("partial listener was not cleaned up: LAN=%+v tailnet=%+v", lan, tailnet)
	}
	if lan.identityOnly {
		t.Fatal("failed enable left the legacy listener in tailnet identity-only mode")
	}
}

func TestDisableStopsBothListenersAndClearsAdditiveMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mobile", "config.json")
	if err := mobilebridge.Save(path, mobilebridge.State{Enabled: true, AlsoTailnet: true}); err != nil {
		t.Fatal(err)
	}
	lan := &fakeLAN{running: true, identityOnly: true}
	tailnet := &fixedTailnetFake{fakeLAN: fakeLAN{running: true}}
	b := &BridgeService{LAN: lan, TailnetLAN: tailnet, ConfigPath: path}
	if err := b.Disable(); err != nil {
		t.Fatal(err)
	}
	if lan.Running() || tailnet.Running() || lan.identityOnly || lan.stopCalls != 1 || tailnet.stopCalls != 1 {
		t.Fatalf("listeners not stopped: LAN=%+v tailnet=%+v", lan, tailnet)
	}
	saved, err := mobilebridge.Load(path)
	if err != nil || saved.Enabled || saved.AlsoTailnet {
		t.Fatalf("disabled state=%+v err=%v", saved, err)
	}
}

func TestRestoreOnBootRestoresAdditionalTailnetListener(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mobile", "config.json")
	state := mobilebridge.State{Enabled: true, Password: "mobile-pass", AlsoTailnet: true}
	if err := mobilebridge.Save(path, state); err != nil {
		t.Fatal(err)
	}
	lan := &legacyPortFake{fakeLAN: fakeLAN{}, bound: 57451}
	tailnet := &fixedTailnetFake{}
	b := &BridgeService{
		LAN: lan, TailnetLAN: tailnet, ConfigPath: path,
		PickTailscaleHosts: func() []string { return []string{"100.64.0.10"} },
	}
	if err := b.RestoreOnBoot(state); err != nil {
		t.Fatal(err)
	}
	if !lan.Running() || !tailnet.Running() || tailnet.requested != mobilebridge.DefaultPort {
		t.Fatalf("restore failed: LAN=%+v tailnet=%+v", lan, tailnet)
	}
	if got := b.Status().TailnetListenerPort; got != mobilebridge.DefaultPort {
		t.Fatalf("tailnet port=%d", got)
	}
}
