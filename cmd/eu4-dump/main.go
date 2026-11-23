package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"eu4rpc/internal/config"
	"eu4rpc/internal/discord"
	"eu4rpc/internal/eu4"
	"eu4rpc/internal/loc"
	"eu4rpc/internal/presence"
)

type dumpOutput struct {
	ExePath      string            `json:"exe_path,omitempty"`
	ResolvedRVAs eu4.ResolvedRVAs  `json:"resolved_rvas"`
	GameState    *eu4.GameState    `json:"game_state"`
	Activity     *discord.Activity `json:"activity"`
}

func main() {
	cfgPath := flag.String("config", "", "Optional path to config.json")
	watch := flag.Bool("watch", false, "Continuously poll every 2 seconds")
	flag.Parse()

	var cfg *config.Config
	path := *cfgPath
	if path == "" {
		path = config.DefaultPath()
	}
	if _, err := os.Stat(path); err == nil {
		if loaded, err := config.Load(path); err == nil {
			cfg = loaded
		}
	}
	if cfg == nil {
		cfg = config.Default()
	}

	locStore, err := loc.LoadFromEU4Dir(cfg.EU4InstallDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not load localization from %s: %v\n", cfg.EU4InstallDir, err)
	}

	reader := eu4.NewReader(locStore)
	defer reader.Close()

	printOnce := func() bool {
		st, err := reader.ReadState()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return false
		}
		act := presence.Format(st, cfg)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		out := dumpOutput{
			ExePath:      reader.ExePath(),
			ResolvedRVAs: reader.RVAs(),
			GameState:    st,
			Activity:     act,
		}
		if err := enc.Encode(out); err != nil {
			fmt.Fprintf(os.Stderr, "encode error: %v\n", err)
			return false
		}
		return true
	}

	if !*watch {
		if !printOnce() {
			os.Exit(1)
		}
		return
	}

	for {
		printOnce()
		time.Sleep(2 * time.Second)
	}
}
