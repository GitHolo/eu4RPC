package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const DefaultClientIDPlaceholder = "YOUR_DISCORD_APPLICATION_ID_HERE"

type AssetsConfig struct {
	Logo             string `json:"logo"`
	AtWar            string `json:"at_war"`
	Paused           string `json:"paused"`
	Ironman          string `json:"ironman"`
	AgeOfDiscovery   string `json:"age_of_discovery"`
	AgeOfReformation string `json:"age_of_reformation"`
	AgeOfAbsolutism  string `json:"age_of_absolutism"`
	AgeOfRevolutions string `json:"age_of_revolutions"`
}

type Config struct {
	ClientID               string       `json:"client_id"`
	EU4InstallDir          string       `json:"eu4_install_dir"`
	UpdateIntervalSeconds  int          `json:"update_interval_seconds"`
	PollIntervalSeconds    int          `json:"poll_interval_seconds"`
	FlagBaseURL            string       `json:"flag_base_url"`
	UseDevPortalFlagAssets bool         `json:"use_dev_portal_flag_assets"`
	Assets                 AssetsConfig `json:"assets"`
}

func Default() *Config {
	return &Config{
		ClientID:              DefaultClientIDPlaceholder,
		EU4InstallDir:         `D:\Steam\steamapps\common\Europa Universalis IV`,
		UpdateIntervalSeconds: 60,
		PollIntervalSeconds:   2,
		FlagBaseURL:           "",
		Assets: AssetsConfig{
			Logo:             "eu4_logo",
			AtWar:            "at_war",
			Paused:           "paused",
			Ironman:          "ironman",
			AgeOfDiscovery:   "age_of_discovery",
			AgeOfReformation: "age_of_reformation",
			AgeOfAbsolutism:  "age_of_absolutism",
			AgeOfRevolutions: "age_of_revolutions",
		},
	}
}

func DefaultPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "config.json"
	}
	return filepath.Join(filepath.Dir(exe), "config.json")
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg := Default()
			_ = cfg.Save(path)
			return cfg, nil
		}
		return nil, err
	}

	cfg := Default()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	cfg.ApplyDefaults()
	return cfg, nil
}

func (c *Config) ApplyDefaults() {
	def := Default()
	c.ClientID = strings.TrimSpace(c.ClientID)
	if c.ClientID == "" {
		c.ClientID = def.ClientID
	}
	c.EU4InstallDir = strings.TrimSpace(c.EU4InstallDir)
	if c.EU4InstallDir == "" {
		c.EU4InstallDir = def.EU4InstallDir
	}
	if c.UpdateIntervalSeconds <= 0 {
		c.UpdateIntervalSeconds = def.UpdateIntervalSeconds
	}
	if c.PollIntervalSeconds <= 0 {
		c.PollIntervalSeconds = def.PollIntervalSeconds
	}
	c.FlagBaseURL = strings.TrimSpace(c.FlagBaseURL)
	if c.Assets.Logo == "" {
		c.Assets.Logo = def.Assets.Logo
	}
	if c.Assets.AtWar == "" {
		c.Assets.AtWar = def.Assets.AtWar
	}
	if c.Assets.Paused == "" {
		c.Assets.Paused = def.Assets.Paused
	}
	if c.Assets.Ironman == "" {
		c.Assets.Ironman = def.Assets.Ironman
	}
	if c.Assets.AgeOfDiscovery == "" {
		c.Assets.AgeOfDiscovery = def.Assets.AgeOfDiscovery
	}
	if c.Assets.AgeOfReformation == "" {
		c.Assets.AgeOfReformation = def.Assets.AgeOfReformation
	}
	if c.Assets.AgeOfAbsolutism == "" {
		c.Assets.AgeOfAbsolutism = def.Assets.AgeOfAbsolutism
	}
	if c.Assets.AgeOfRevolutions == "" {
		c.Assets.AgeOfRevolutions = def.Assets.AgeOfRevolutions
	}
}

func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}

func (c *Config) HasValidClientID() bool {
	if c == nil {
		return false
	}
	id := strings.TrimSpace(c.ClientID)
	return id != "" && id != DefaultClientIDPlaceholder
}

func (c *Config) AgeAsset(ageKey string) string {
	switch ageKey {
	case "age_of_discovery":
		return c.Assets.AgeOfDiscovery
	case "age_of_reformation":
		return c.Assets.AgeOfReformation
	case "age_of_absolutism":
		return c.Assets.AgeOfAbsolutism
	case "age_of_revolutions":
		return c.Assets.AgeOfRevolutions
	default:
		return ""
	}
}

func (c *Config) FlagURL(tag string) string {
	if c.FlagBaseURL == "" || tag == "" {
		return ""
	}
	u := strings.ReplaceAll(c.FlagBaseURL, "{TAG}", strings.ToUpper(tag))
	u = strings.ReplaceAll(u, "{tag}", strings.ToLower(tag))
	return u
}
