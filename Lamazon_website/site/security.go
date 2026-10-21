package site

import (
	"net/http"
	"strings"
)

// Origin checks happen before cookies can be converted into bearer credentials
// by the shopper/staff proxies. Non-browser API clients remain supported.
func securityHeaders(next http.Handler) http.Handler {
	crossOrigin := http.NewCrossOriginProtection()
	// The stateless public storefront proxies to the credentialed payment BFF.
	// Its original browser Origin remains trusted at that second hop.
	for _, origin := range []string{"https://www.uniminute.in", "https://uniminute.in", "https://uniminute.vercel.app", "https://api.geltrax.engineer"} {
		_ = crossOrigin.AddTrustedOrigin(origin)
	}
	crossOrigin.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paymentError(w, 403, "This request came from another website. Reload Uniminute and try again.")
	}))
	protected := crossOrigin.Handler(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			limit := int64(1 << 20)
			if strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
				limit = 40 << 20
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(self)")
		// Alpine's existing expression evaluator needs unsafe-eval; a CSP-safe Alpine
		// migration is separate. External scripts/frames remain allow-listed.
		h.Set("Content-Security-Policy", "default-src 'self'; base-uri 'self'; object-src 'none'; frame-ancestors 'none'; form-action 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval' https://*.razorpay.com https://www.gstatic.com https://www.googleapis.com; style-src 'self' 'unsafe-inline'; img-src 'self' https: data: blob:; font-src 'self' data:; media-src 'self' https: blob:; connect-src 'self' https://*.razorpay.com wss://*.razorpay.com https://*.googleapis.com https://*.firebaseio.com wss://*.firebaseio.com https://*.firebasedatabase.app https://*.cloudinary.com; frame-src https://*.razorpay.com https://*.google.com; worker-src 'self' blob:")
		if requestIsHTTPS(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		protected.ServeHTTP(&secureCookieWriter{ResponseWriter: w, secure: requestIsHTTPS(r)}, r)
	})
}

type secureCookieWriter struct {
	http.ResponseWriter
	secure, written bool
}

func (w *secureCookieWriter) WriteHeader(status int) {
	if w.written {
		return
	}
	w.written = true
	if w.secure {
		values := w.Header().Values("Set-Cookie")
		w.Header().Del("Set-Cookie")
		for _, v := range values {
			if !strings.Contains(strings.ToLower(v), "; secure") {
				v += "; Secure"
			}
			w.Header().Add("Set-Cookie", v)
		}
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *secureCookieWriter) Write(body []byte) (int, error) {
	if !w.written {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(body)
}
func (w *secureCookieWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
