package shop

// Money writes a rupee amount the way it is read here: grouped in lakhs, and
// with the paise left off when there are none. It mirrors MoneyText in the app
// (frontend/lib/data/money.dart) and the storefront's own money() one-for-one,
// so a price never changes shape between the app and this website.
import (
	"fmt"
	"strings"
)

func Money(v float64) string {
	paise := int64(v*100 + 0.5)
	whole, rem := paise/100, paise%100
	digits := fmt.Sprintf("%d", whole)
	if len(digits) > 3 {
		head, tail := digits[:len(digits)-3], digits[len(digits)-3:]
		var parts []string
		for len(head) > 2 {
			parts = append([]string{head[len(head)-2:]}, parts...)
			head = head[:len(head)-2]
		}
		if head != "" {
			parts = append([]string{head}, parts...)
		}
		digits = strings.Join(parts, ",") + "," + tail
	}
	if rem == 0 {
		return digits
	}
	return fmt.Sprintf("%s.%02d", digits, rem)
}

// Itoa converts an int to its decimal string representation. It avoids
// importing strconv so the shop package stays dependency-free.
func Itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	if neg {
		return "-" + digits
	}
	return digits
}

// SpokenTitle cuts a card title the way SpokenTitle does in the app: a legacy
// product can carry a 194-character name, and cards are scanned, not read.
func SpokenTitle(title string) string {
	title = strings.TrimSpace(title)
	const limit = 72
	if len(title) <= limit {
		return title
	}
	cut := strings.LastIndex(title[:limit], " ")
	if cut < 40 {
		cut = limit
	}
	return strings.TrimRight(title[:cut], " ") + "…"
}
