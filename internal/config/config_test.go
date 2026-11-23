package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCreatesDefaultWithEmptyInstallDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.EU4InstallDir != "" {
		t.Fatalf("expected empty EU4InstallDir by default, got %q", cfg.EU4InstallDir)
	}
	if cfg.HasValidClientID() {
		t.Fatal("expected placeholder client ID to be invalid")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected Load to create %s: %v", path, err)
	}
}

func TestLoadMergesPartialConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	raw := `{"client_id":" 123456 ","eu4_install_dir":"   ","update_interval_seconds":0,"flag_base_url":"https://cdn.example/{tag}.png"}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if !cfg.HasValidClientID() || cfg.ClientID != "123456" {
		t.Fatalf("unexpected client ID: %q", cfg.ClientID)
	}
	if cfg.EU4InstallDir != "" {
		t.Fatalf("expected blank install dir to stay empty, got %q", cfg.EU4InstallDir)
	}
	if cfg.UpdateIntervalSeconds != 60 {
		t.Fatalf("expected default UpdateIntervalSeconds=60, got %d", cfg.UpdateIntervalSeconds)
	}
	if got := cfg.FlagURL("RUS"); got != "https://cdn.example/rus.png" {
		t.Fatalf("unexpected FlagURL: %q", got)
	}
	if got := cfg.AgeAsset("age_of_absolutism"); got != "age_of_absolutism" {
		t.Fatalf("unexpected AgeAsset: %q", got)
	}
}
