package loc

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromEU4DirAndCleanCodes(t *testing.T) {
	tmp := t.TempDir()
	locDir := filepath.Join(tmp, "localisation")
	repDir := filepath.Join(locDir, "replace")
	if err := os.MkdirAll(repDir, 0o755); err != nil {
		t.Fatal(err)
	}

	countries := "\xEF\xBB\xBFl_english:\n RUS:0 \"Russia\"\n MUN:0 \"§YMünster§!\"\n FRA:0 \"France\" # previously \"Francia\"\n HAB:0 \"The \\\"Habsburg\\\" Realm\"\n"
	ages := "\xEF\xBB\xBFl_english:\n age_of_discovery:0 \"Age of Discovery\"\n age_of_reformation:0 \"Age of Reformation\"\n"
	replace := "\xEF\xBB\xBFl_english:\n RUS:0 \"Tsardom of Russia\"\n"

	if err := os.WriteFile(filepath.Join(locDir, "countries_l_english.yml"), []byte(countries), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locDir, "EU4_l_english.yml"), []byte(ages), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repDir, "custom_overrides_l_english.yml"), []byte(replace), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := LoadFromEU4Dir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if got := store.LookupCountry("rus"); got != "Tsardom of Russia" {
		t.Fatalf("expected Tsardom of Russia, got %q", got)
	}
	if got := store.LookupCountry("MUN"); got != "Münster" {
		t.Fatalf("expected Münster, got %q", got)
	}
	if got := store.LookupCountry("FRA"); got != "France" {
		t.Fatalf("expected France, got %q", got)
	}
	if got := store.LookupCountry("HAB"); got != `The "Habsburg" Realm` {
		t.Fatalf("expected unescaped quote name, got %q", got)
	}
	if got := store.LookupAge("age_of_reformation"); got != "Age of Reformation" {
		t.Fatalf("expected Age of Reformation, got %q", got)
	}
	if got := store.LookupAge("age_of_custom_era"); got != "Age of Custom Era" {
		t.Fatalf("expected humanized fallback, got %q", got)
	}
}
