package loc

import (
	"bufio"
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	reTagKey       = regexp.MustCompile(`^[A-Z0-9]{3}$`)
	reColorCode    = regexp.MustCompile(`§.`)
	reInlineIcon   = regexp.MustCompile(`£[^£]+£`)
	defaultAgeKeys = map[string]string{
		"age_of_discovery":   "Age of Discovery",
		"age_of_reformation": "Age of Reformation",
		"age_of_absolutism":  "Age of Absolutism",
		"age_of_revolutions": "Age of Revolutions",
	}
)

type Store struct {
	Countries map[string]string
	Entries   map[string]string
}

func NewEmpty() *Store {
	s := &Store{
		Countries: make(map[string]string),
		Entries:   make(map[string]string),
	}
	for k, v := range defaultAgeKeys {
		s.Entries[k] = v
	}
	return s
}

func LoadFromEU4Dir(eu4Dir string) (*Store, error) {
	s := NewEmpty()
	if eu4Dir == "" {
		return s, nil
	}
	locDir := filepath.Join(eu4Dir, "localisation")
	info, err := os.Stat(locDir)
	if err != nil || !info.IsDir() {
		return s, err
	}

	var normalFiles []string
	var countryFiles []string
	var replaceFiles []string

	err = filepath.WalkDir(locDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		lower := strings.ToLower(d.Name())
		if !strings.HasSuffix(lower, "_l_english.yml") {
			return nil
		}
		rel, _ := filepath.Rel(locDir, path)
		relLower := strings.ToLower(filepath.ToSlash(rel))
		if strings.HasPrefix(relLower, "replace/") {
			replaceFiles = append(replaceFiles, path)
		} else if strings.Contains(lower, "countries") {
			countryFiles = append(countryFiles, path)
		} else {
			normalFiles = append(normalFiles, path)
		}
		return nil
	})
	if err != nil {
		return s, err
	}

	sort.Strings(normalFiles)
	sort.Strings(countryFiles)
	sort.Strings(replaceFiles)

	for _, p := range normalFiles {
		_ = s.loadFile(p, false)
	}
	for _, p := range countryFiles {
		_ = s.loadFile(p, true)
	}
	for _, p := range replaceFiles {
		_ = s.loadFile(p, true)
	}
	return s, nil
}

func (s *Store) loadFile(path string, countryPriority bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	scanner := bufio.NewScanner(bytes.NewReader(data))
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		key, val, ok := parseLine(scanner.Text())
		if !ok {
			continue
		}
		if reTagKey.MatchString(key) {
			if _, exists := s.Countries[key]; countryPriority || !exists {
				s.Countries[key] = val
			}
		}
		if strings.HasPrefix(key, "age_") {
			s.Entries[key] = val
		}
	}
	return scanner.Err()
}

func parseLine(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}
	colon := strings.IndexByte(trimmed, ':')
	if colon <= 0 {
		return "", "", false
	}
	key := strings.TrimSpace(trimmed[:colon])
	if key == "" || strings.ContainsAny(key, " \t") {
		return "", "", false
	}
	rest := trimmed[colon+1:]
	firstQuote := strings.IndexByte(rest, '"')
	if firstQuote < 0 {
		return "", "", false
	}
	endQuote := -1
	for i := firstQuote + 1; i < len(rest); i++ {
		if rest[i] == '\\' {
			i++
			continue
		}
		if rest[i] == '"' {
			endQuote = i
			break
		}
	}
	if endQuote <= firstQuote {
		return "", "", false
	}
	val := rest[firstQuote+1 : endQuote]
	val = cleanLocText(val)
	if val == "" {
		return "", "", false
	}
	return key, val, true
}

func cleanLocText(s string) string {
	s = strings.ReplaceAll(s, `\"`, `"`)
	s = strings.ReplaceAll(s, `\n`, " ")
	s = reColorCode.ReplaceAllString(s, "")
	s = reInlineIcon.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

func (s *Store) LookupCountry(tag string) string {
	if s == nil {
		return tag
	}
	upper := strings.ToUpper(strings.TrimSpace(tag))
	if name, ok := s.Countries[upper]; ok && name != "" {
		return name
	}
	return tag
}

func (s *Store) LookupAge(ageKey string) string {
	if ageKey == "" {
		return ""
	}
	if s != nil {
		if name, ok := s.Entries[ageKey]; ok && name != "" {
			return name
		}
	}
	if name, ok := defaultAgeKeys[ageKey]; ok {
		return name
	}
	return humanizeKey(ageKey)
}

func humanizeKey(k string) string {
	parts := strings.Split(strings.TrimSpace(k), "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		if i > 0 && (p == "of" || p == "the" || p == "and") {
			parts[i] = p
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}
