package main

import (
	"fmt"
	"html"
	"strings"
)

// The logo lives in Cloudinary because email clients will not render an
// attachment inline reliably, and most block remote images until the reader
// allows them — so nothing important is ever only in a picture.
// logoURL is the clock mark the website serves; it changes with the site, not
// with an image someone has to remember to upload elsewhere.
const logoURL = "https://uniminute.vercel.app/static/images/logo.png"

// emailHTML wraps content in the shell every Uniminute email shares.
//
// Written with tables and inline styles on purpose: Gmail strips <style>
// blocks, Outlook ignores flexbox, and a plain <div> layout collapses in
// both. This is the boring markup that survives.
func emailHTML(heading, body, footer string) string {
	return fmt.Sprintf(`<!doctype html>
<html><body style="margin:0;padding:0;background:#F1F1EF;">
  <table role="presentation" width="100%%" cellpadding="0" cellspacing="0"
         style="background:#F1F1EF;padding:32px 12px;">
    <tr><td align="center">
      <table role="presentation" width="100%%" cellpadding="0" cellspacing="0"
             style="max-width:480px;background:#ffffff;border-radius:18px;
                    font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',
                    Roboto,Helvetica,Arial,sans-serif;">
        <tr><td style="padding:28px 28px 0 28px;">
          <img src="%s" width="44" height="44" alt="Uniminute"
               style="display:block;border-radius:10px;">
        </td></tr>
        <tr><td style="padding:18px 28px 0 28px;">
          <h1 style="margin:0;font-size:19px;line-height:1.3;color:#1A1A1A;
                     font-weight:700;">%s</h1>
        </td></tr>
        <tr><td style="padding:12px 28px 26px 28px;font-size:14px;
                       line-height:1.55;color:#4A4A4A;">%s</td></tr>
        <tr><td style="padding:0 28px 26px 28px;">
          <div style="border-top:1px solid #EDEDEA;padding-top:14px;
                      font-size:11.5px;line-height:1.5;color:#9A9A9A;">%s</div>
        </td></tr>
      </table>
      <div style="max-width:480px;padding:14px 6px;font-size:11px;
                  color:#A5A5A0;font-family:-apple-system,BlinkMacSystemFont,
                  'Segoe UI',Roboto,Helvetica,Arial,sans-serif;">
        Uniminute · Lovely Professional University
      </div>
    </td></tr>
  </table>
</body></html>`, logoURL, html.EscapeString(heading), body, html.EscapeString(footer))
}

// codeHTML is the sign-in email. The code is the whole message, so it is set
// large and spaced — people read it off the notification and type it.
func codeHTML(code string) string {
	body := fmt.Sprintf(`
    <p style="margin:0 0 16px 0;">Use this code to sign in:</p>
    <div style="background:#F1F1EF;border-radius:12px;padding:16px;
                text-align:center;font-size:30px;letter-spacing:7px;
                font-weight:700;color:#1A1A1A;">%s</div>
    <p style="margin:16px 0 0 0;">It expires in 10 minutes and can be used
       once.</p>`, html.EscapeString(code))
	return emailHTML("Your sign-in code",
		body,
		"If you did not ask to sign in, ignore this email — nobody can get in without the code.")
}

func resetHTML(code string) string {
	body := fmt.Sprintf(`
    <p style="margin:0 0 16px 0;">Use this code to set a new password:</p>
    <div style="background:#F1F1EF;border-radius:12px;padding:16px;
                text-align:center;font-size:30px;letter-spacing:7px;
                font-weight:700;color:#1A1A1A;">%s</div>
    <p style="margin:16px 0 0 0;">It expires in 10 minutes and can be used
       once.</p>`, html.EscapeString(code))
	return emailHTML("Reset your password",
		body,
		"If you did not ask to reset your password, ignore this email — your password has not changed.")
}

// notifyHTML is the shell every other notification uses: a heading and the
// same body text the plain-text part carries, so the two never disagree.
func notifyHTML(title, text string) string {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		fmt.Fprintf(&b, `<p style="margin:0 0 10px 0;">%s</p>`, html.EscapeString(line))
	}
	return emailHTML(title, b.String(), "You get this because you have an account on Uniminute. You can turn these emails off in Settings.")
}
