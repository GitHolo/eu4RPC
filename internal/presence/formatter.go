package presence

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"eu4rpc/internal/config"
	"eu4rpc/internal/discord"
	"eu4rpc/internal/eu4"
)

const maxDiscordFieldLen = 128

func Format(st *eu4.GameState, cfg *config.Config) *discord.Activity {
	if st == nil {
		return nil
	}
	if cfg == nil {
		cfg = config.Default()
	}

	act := &discord.Activity{}
	if st.SessionStartUnix > 0 {
		act.Timestamps = &discord.Timestamps{Start: st.SessionStartUnix}
	}

	switch st.Mode {
	case eu4.ModeSPLobby:
		act.Details = "In Singleplayer Lobby"
		act.State = "Choosing a Nation"
		act.Assets = &discord.Assets{
			LargeImage: cfg.Assets.Logo,
			LargeText:  "Europa Universalis IV",
		}
	case eu4.ModeMPLobby:
		act.Details = "In Multiplayer Lobby"
		if st.NumPlayers > 0 {
			act.State = fmt.Sprintf("Multiplayer (%d)", st.NumPlayers)
		} else {
			act.State = "Waiting for Players"
		}
		act.Assets = &discord.Assets{
			LargeImage: cfg.Assets.Logo,
			LargeText:  "Europa Universalis IV",
		}
	case eu4.ModeInGame, eu4.ModeObserving:
		formatInGame(act, st, cfg)
	default:
		act.Details = "In Main Menu"
		act.State = "Europa Universalis IV"
		act.Assets = &discord.Assets{
			LargeImage: cfg.Assets.Logo,
			LargeText:  "Europa Universalis IV",
		}
	}

	act.Details = clampDiscordText(act.Details)
	act.State = clampDiscordText(act.State)
	if act.Assets != nil {
		act.Assets.LargeText = clampDiscordText(act.Assets.LargeText)
		act.Assets.SmallText = clampDiscordText(act.Assets.SmallText)
	}
	return act
}

func formatInGame(act *discord.Activity, st *eu4.GameState, cfg *config.Config) {
	country := strings.TrimSpace(st.CountryName)
	if country == "" {
		country = strings.TrimSpace(st.CountryTag)
	}

	if st.Mode == eu4.ModeObserving {
		if country != "" {
			act.Details = "Observing " + country
		} else {
			act.Details = "Observing"
		}
	} else {
		if country == "" {
			country = "Unknown Nation"
		}
		act.Details = "Playing as " + country
	}

	var modeText string
	if st.IsMultiplayer {
		if st.NumPlayers > 0 {
			modeText = fmt.Sprintf("Multiplayer (%d)", st.NumPlayers)
		} else {
			modeText = "Multiplayer"
		}
	} else if st.IsIronman {
		modeText = "Ironman"
	} else {
		modeText = "Singleplayer"
	}

	if st.Year > 0 {
		act.State = fmt.Sprintf("%d • %s", st.Year, modeText)
	} else {
		act.State = modeText
	}

	assets := &discord.Assets{
		LargeImage: resolveLargeImage(st, cfg),
	}

	if st.CountryTag != "" && country != "" {
		var largeParts []string
		if st.GreatPowerRank > 0 {
			largeParts = append(largeParts, fmt.Sprintf("%s (GP #%d)", country, st.GreatPowerRank))
		} else {
			largeParts = append(largeParts, country)
		}
		if st.TotalDev > 0 || st.NumProvinces > 0 {
			largeParts = append(largeParts, formatIntComma(st.TotalDev)+" Dev")
			provLabel := "Provinces"
			if st.NumProvinces == 1 {
				provLabel = "Province"
			}
			largeParts = append(largeParts, formatIntComma(st.NumProvinces)+" "+provLabel)
		}
		assets.LargeText = strings.Join(largeParts, " • ")
	} else if st.Mode == eu4.ModeObserving {
		assets.LargeText = "Observer Mode"
	} else {
		assets.LargeText = "Europa Universalis IV"
	}

	assets.SmallImage = resolveSmallImage(st, cfg)

	var smallParts []string
	if st.AgeName != "" {
		smallParts = append(smallParts, st.AgeName)
	}
	if st.CountryTag != "" {
		if st.IsAtWar {
			if st.PrimaryEnemy != "" && st.WarCount <= 1 {
				smallParts = append(smallParts, "At War with "+st.PrimaryEnemy)
			} else if st.WarCount > 1 {
				smallParts = append(smallParts, fmt.Sprintf("At War (%d)", st.WarCount))
			} else {
				smallParts = append(smallParts, "At War")
			}
		} else {
			smallParts = append(smallParts, "At Peace")
		}
	}
	if st.IsIronman && st.IsMultiplayer {
		smallParts = append(smallParts, "Ironman")
	}
	if st.IsPaused {
		smallParts = append(smallParts, "Paused")
	} else if st.GameSpeed > 0 {
		smallParts = append(smallParts, fmt.Sprintf("Speed %d", st.GameSpeed))
	}
	assets.SmallText = strings.Join(smallParts, " • ")
	if assets.SmallImage == "" {
		assets.SmallText = ""
	}

	act.Assets = assets
}

func resolveLargeImage(st *eu4.GameState, cfg *config.Config) string {
	if st.CountryTag != "" {
		if url := cfg.FlagURL(st.CountryTag); url != "" {
			return url
		}
	}
	return cfg.Assets.Logo
}

func resolveSmallImage(st *eu4.GameState, cfg *config.Config) string {
	if st.IsPaused {
		return cfg.Assets.Paused
	}
	if st.IsAtWar {
		return cfg.Assets.AtWar
	}
	if ageKey := cfg.AgeAsset(st.AgeKey); ageKey != "" {
		return ageKey
	}
	if st.IsIronman {
		return cfg.Assets.Ironman
	}
	return ""
}

func formatIntComma(n int) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		if neg {
			return "-" + s
		}
		return s
	}
	var out strings.Builder
	if neg {
		out.WriteByte('-')
	}
	rem := len(s) % 3
	if rem > 0 {
		out.WriteString(s[:rem])
		if len(s) > rem {
			out.WriteByte(',')
		}
	}
	for i := rem; i < len(s); i += 3 {
		if i > rem {
			out.WriteByte(',')
		}
		out.WriteString(s[i : i+3])
	}
	return out.String()
}

func clampDiscordText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if utf8.RuneCountInString(s) < 2 {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxDiscordFieldLen {
		return s
	}
	runes := []rune(s)
	return strings.TrimSpace(string(runes[:maxDiscordFieldLen-1])) + "…"
}

type Controller struct {
	hasPushed       bool
	lastPushAt      time.Time
	lastPID         uint32
	lastMode        eu4.GameMode
	lastCountryTag  string
	lastCountryName string
	lastActivity    *discord.Activity
}

func NewController() *Controller {
	return &Controller{}
}

func (c *Controller) Reset() {
	c.hasPushed = false
	c.lastPushAt = time.Time{}
	c.lastPID = 0
	c.lastMode = ""
	c.lastCountryTag = ""
	c.lastCountryName = ""
	c.lastActivity = nil
}

func (c *Controller) ShouldPush(st *eu4.GameState, act *discord.Activity, now time.Time, force bool, interval time.Duration) (bool, string) {
	if st == nil || act == nil {
		return false, ""
	}
	if force {
		return true, "manual refresh"
	}
	if !c.hasPushed {
		return true, "session start"
	}
	if st.PID != c.lastPID {
		return true, "process changed"
	}
	if st.Mode != c.lastMode {
		return true, fmt.Sprintf("mode %s -> %s", c.lastMode, st.Mode)
	}
	if st.CountryTag != c.lastCountryTag || st.CountryName != c.lastCountryName {
		return true, "country changed"
	}
	if interval <= 0 {
		interval = 60 * time.Second
	}
	if now.Sub(c.lastPushAt) >= interval && !act.Equal(c.lastActivity) {
		return true, "periodic update"
	}
	return false, ""
}

func (c *Controller) MarkPushed(st *eu4.GameState, act *discord.Activity, now time.Time) {
	c.hasPushed = true
	c.lastPushAt = now
	c.lastPID = st.PID
	c.lastMode = st.Mode
	c.lastCountryTag = st.CountryTag
	c.lastCountryName = st.CountryName
	c.lastActivity = cloneActivity(act)
}

func cloneActivity(a *discord.Activity) *discord.Activity {
	if a == nil {
		return nil
	}
	cp := *a
	if a.Timestamps != nil {
		ts := *a.Timestamps
		cp.Timestamps = &ts
	}
	if a.Assets != nil {
		as := *a.Assets
		cp.Assets = &as
	}
	return &cp
}
