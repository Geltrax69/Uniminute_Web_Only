package main

import (
	"net/http"
	"time"
)

type Notification struct {
	ID        int64      `json:"id"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Target    string     `json:"target"`
	CreatedAt time.Time  `json:"createdAt"`
	ReadAt    *time.Time `json:"readAt,omitempty"`
}

// GET /api/notifications — the signed-in person's latest 50, newest first,
// with how many are still unread.
func (a *API) handleNotifications(w http.ResponseWriter, r *http.Request) {
	email := a.owner(r)
	rows, err := a.db.sql.QueryContext(r.Context(), `
		SELECT id, title, body, target, created_at, read_at FROM notifications
		WHERE email = $1 ORDER BY created_at DESC LIMIT 50`, email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := struct {
		Unread int            `json:"unread"`
		Items  []Notification `json:"items"`
	}{Items: []Notification{}}
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.Title, &n.Body, &n.Target, &n.CreatedAt, &n.ReadAt); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out.Items = append(out.Items, n)
	}
	a.db.sql.QueryRowContext(r.Context(),
		`SELECT count(*) FROM notifications WHERE email = $1 AND read_at IS NULL`, email).Scan(&out.Unread)
	writeJSON(w, http.StatusOK, out)
}

// POST /api/notifications/read — marks them all read.
func (a *API) handleNotificationsRead(w http.ResponseWriter, r *http.Request) {
	if _, err := a.db.sql.ExecContext(r.Context(),
		`UPDATE notifications SET read_at = now() WHERE email = $1 AND read_at IS NULL`, a.owner(r)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
