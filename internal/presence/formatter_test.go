package presence

import (
	"testing"
	"time"

	"eu4rpc/internal/config"
	"eu4rpc/internal/eu4"
)

func TestFormatInGameYearOnlyAndTooltips(t *testing.T) {
	cfg := config.Default()
	cfg.FlagBaseURL = "https://example.com/flags/{TAG}.png"

	st := &eu4.GameState{
		PID:              1234,
		SessionStartUnix: 1700000000,
		Mode:             eu4.ModeInGame,
		Year:             1561,
		CountryTag:       "RUS",
		CountryName:      "Russia",
		AgeKey:           "age_of_reformation",
		AgeName:          "Age of Reformation",
		IsMultiplayer:    true,
		NumPlayers:       4,
		IsPaused:         false,
		GameSpeed:        3,
		TotalDev:         1420,
		NumProvinces:     169,
		GreatPowerRank:   1,
		IsAtWar:          true,
		WarCount:         1,
		PrimaryEnemy:     "Ottomans",
	}

	act := Format(st, cfg)
	if act.Details != "Playing as Russia" {
		t.Fatalf("unexpected details: %q", act.Details)
	}
	if act.State != "1561 • Multiplayer (4)" {
		t.Fatalf("unexpected state: %q", act.State)
	}
	if act.Timestamps == nil || act.Timestamps.Start != 1700000000 {
		t.Fatalf("unexpected timestamps: %+v", act.Timestamps)
	}
	if act.Assets.LargeImage != "https://example.com/flags/RUS.png" {
		t.Fatalf("unexpected large image: %q", act.Assets.LargeImage)
	}
	if act.Assets.LargeText != "Russia (GP #1) • 1,420 Dev • 169 Provinces" {
		t.Fatalf("unexpected large text: %q", act.Assets.LargeText)
	}
	if act.Assets.SmallImage != "at_war" {
		t.Fatalf("unexpected small image: %q", act.Assets.SmallImage)
	}
	if act.Assets.SmallText != "Age of Reformation • At War with Ottomans • Speed 3" {
		t.Fatalf("unexpected small text: %q", act.Assets.SmallText)
	}
}

func TestFormatMenusAndObserving(t *testing.T) {
	cfg := config.Default()

	menu := Format(&eu4.GameState{Mode: eu4.ModeMainMenu, SessionStartUnix: 100}, cfg)
	if menu.Details != "In Main Menu" || menu.State != "Europa Universalis IV" || menu.Assets.SmallImage != "" {
		t.Fatalf("unexpected menu payload: %+v %+v", menu, menu.Assets)
	}

	sp := Format(&eu4.GameState{Mode: eu4.ModeSPLobby, SessionStartUnix: 100}, cfg)
	if sp.Details != "In Singleplayer Lobby" || sp.State != "Choosing a Nation" {
		t.Fatalf("unexpected SP lobby payload: %+v", sp)
	}

	obs := Format(&eu4.GameState{
		Mode:             eu4.ModeObserving,
		Year:             1453,
		AgeKey:           "age_of_discovery",
		AgeName:          "Age of Discovery",
		IsPaused:         true,
		SessionStartUnix: 100,
	}, cfg)
	if obs.Details != "Observing" || obs.State != "1453 • Singleplayer" || obs.Assets.SmallImage != "paused" {
		t.Fatalf("unexpected observing payload: %+v %+v", obs, obs.Assets)
	}
}

func TestControllerThrottleAndImmediateTransitions(t *testing.T) {
	cfg := config.Default()
	ctrl := NewController()
	t0 := time.Unix(1700000000, 0)

	st := &eu4.GameState{PID: 1, Mode: eu4.ModeMainMenu, SessionStartUnix: 1700000000}
	act := Format(st, cfg)

	push, _ := ctrl.ShouldPush(st, act, t0, false, 60*time.Second)
	if !push {
		t.Fatal("expected initial push")
	}
	ctrl.MarkPushed(st, act, t0)

	// Same state after 10s -> throttled
	push, _ = ctrl.ShouldPush(st, act, t0.Add(10*time.Second), false, 60*time.Second)
	if push {
		t.Fatal("expected steady-state push to be throttled")
	}

	// Mode transition after 5s -> immediate push
	stGame := &eu4.GameState{
		PID:              1,
		Mode:             eu4.ModeInGame,
		Year:             1444,
		CountryTag:       "MOS",
		CountryName:      "Muscovy",
		SessionStartUnix: 1700000000,
	}
	actGame := Format(stGame, cfg)
	push, _ = ctrl.ShouldPush(stGame, actGame, t0.Add(15*time.Second), false, 60*time.Second)
	if !push {
		t.Fatal("expected immediate push on mode change")
	}
	ctrl.MarkPushed(stGame, actGame, t0.Add(15*time.Second))

	// Year increments 1444 -> 1445 after 30s -> still throttled until 60s
	st1445 := *stGame
	st1445.Year = 1445
	act1445 := Format(&st1445, cfg)
	push, _ = ctrl.ShouldPush(&st1445, act1445, t0.Add(45*time.Second), false, 60*time.Second)
	if push {
		t.Fatal("expected year increment before 60s to be throttled")
	}

	// Same year at 75s (no visual change) -> skip redundant push
	push, _ = ctrl.ShouldPush(stGame, actGame, t0.Add(75*time.Second), false, 60*time.Second)
	if push {
		t.Fatal("expected unchanged activity at 60s tick not to push")
	}

	// Changed year at 75s -> push!
	push, _ = ctrl.ShouldPush(&st1445, act1445, t0.Add(75*time.Second), false, 60*time.Second)
	if !push {
		t.Fatal("expected changed activity after 60s to push")
	}

	// Country change (tag/name formation) -> immediate push
	stForm := st1445
	stForm.CountryTag = "RUS"
	stForm.CountryName = "Russia"
	actForm := Format(&stForm, cfg)
	push, _ = ctrl.ShouldPush(&stForm, actForm, t0.Add(77*time.Second), false, 60*time.Second)
	if !push {
		t.Fatal("expected immediate push on country change")
	}
}
