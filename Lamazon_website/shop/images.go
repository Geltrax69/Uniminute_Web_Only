package shop

// CatalogueImage asks the Cloudinary CDN for the size actually being drawn, in
// the same buckets the app uses so the two frontends share one cache rather
// than doubling it. One-for-one with the backend storefront's catalogueImage —
// presentation, not business logic, but kept identical anyway.
import "fmt"
import "strings"

func CatalogueImage(url string, width int) string {
	const marker = "/image/upload/"
	if url == "" || !strings.Contains(url, marker) {
		return url
	}
	if strings.Contains(url, "c_fill") || strings.Contains(url, "c_pad") ||
		strings.Contains(url, "c_limit") {
		return url
	}
	return strings.Replace(url, marker, fmt.Sprintf(
		"%sc_fill,ar_1:1,g_auto,e_improve:30,w_%d,f_auto,q_auto/", marker, Bucket(width)), 1)
}

func Bucket(width int) int {
	for _, size := range []int{160, 300, 400, 800} {
		if width <= size {
			return size
		}
	}
	return 1200
}

// Thumb and Hero are the two sizes the pages draw.
func Thumb(url string) string { return CatalogueImage(url, 300) }

func Hero(url string) string { return CatalogueImage(url, 800) }
