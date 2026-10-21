package pages

import (
	"strconv"
	"strings"
	"time"
	"unicode"
)

func reportDisplayTime(t time.Time) string {
	return t.In(time.FixedZone("IST", 19800)).Format("02 Jan, 15:04 IST")
}
func ratingLabel(n int) string {
	if n == 0 {
		return "Not reviewed"
	}
	return strconv.Itoa(n) + " / 5"
}

func operationTime(data map[string]map[string]any, section, key string) string {
	value, _ := data[section][key].(string)
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return "No successful check recorded"
	}
	return t.In(time.FixedZone("IST", 19800)).Format("02 Jan 2006, 15:04 IST")
}
func operationLabel(data map[string]map[string]any, kind string) string {
	section, key, flag, maxAge := "backup", "lastBackup", "backupHealthy", 30*time.Hour
	if kind == "restore" {
		key = "lastRestoreCheck"
		flag = "restoreHealthy"
		maxAge = 8 * 24 * time.Hour
	}
	if kind == "health" {
		section = "health"
		key = "checkedAt"
		maxAge = 3 * time.Minute
	}
	info := data[section]
	if info["available"] != true {
		return "Not configured / unavailable"
	}
	raw, _ := info[key].(string)
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return "No successful check recorded"
	}
	if time.Since(t) > maxAge {
		return "Check overdue"
	}
	if kind == "health" {
		if info["api"] == true && info["website"] == true {
			return "Healthy"
		}
		return "Needs attention"
	}
	if info[flag] == true {
		return "Verified"
	}
	return "Needs attention"
}

func safeCSVCell(value string) string {
	trimmed := strings.TrimLeftFunc(value, func(r rune) bool { return unicode.IsSpace(r) || r == '\uFEFF' })
	if strings.HasPrefix(value, "\t") || strings.HasPrefix(value, "\r") || strings.HasPrefix(value, "\n") || len(trimmed) > 0 && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + value
	}
	return value
}

func (d AdminData) CanExportCSV() bool { return d.hasTable() }
