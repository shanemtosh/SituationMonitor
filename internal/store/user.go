package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// User represents a registered user.
type User struct {
	ID        int64
	Username  string
	Email     string
	CreatedAt string
}

// CreateUser registers a new user with a bcrypt-hashed password.
func CreateUser(ctx context.Context, db *sql.DB, username, email, password string) (int64, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return 0, err
	}
	res, err := db.ExecContext(ctx, `
INSERT INTO users (username, email, password) VALUES (?, ?, ?)
`, username, email, string(hash))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// AuthenticateUser checks username+password and returns the user if valid.
func AuthenticateUser(ctx context.Context, db *sql.DB, username, password string) (*User, error) {
	var u User
	var hash string
	err := db.QueryRowContext(ctx, `
SELECT id, username, email, password, created_at FROM users WHERE username = ?
`, username).Scan(&u.ID, &u.Username, &u.Email, &hash, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return nil, fmt.Errorf("invalid password")
	}
	return &u, nil
}

// CreateSession generates a random session token valid for 30 days.
func CreateSession(ctx context.Context, db *sql.DB, userID int64) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	expires := time.Now().UTC().Add(30 * 24 * time.Hour).Format(time.RFC3339)
	_, err := db.ExecContext(ctx, `
INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)
`, token, userID, expires)
	return token, err
}

// GetSession looks up a session token and returns the user if valid.
func GetSession(ctx context.Context, db *sql.DB, token string) (*User, error) {
	var u User
	err := db.QueryRowContext(ctx, `
SELECT u.id, u.username, u.email, u.created_at
FROM sessions s JOIN users u ON u.id = s.user_id
WHERE s.token = ? AND datetime(s.expires_at) > datetime('now')
`, token).Scan(&u.ID, &u.Username, &u.Email, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// DeleteSession removes a session token (logout).
func DeleteSession(ctx context.Context, db *sql.DB, token string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, token)
	return err
}

// UserAction represents a saved action (save, highlight, note, hide_feed).
type UserAction struct {
	ID        int64           `json:"id"`
	UserID    int64           `json:"user_id"`
	ItemID    *int64          `json:"item_id,omitempty"`
	Action    string          `json:"action"`
	Data      json.RawMessage `json:"data,omitempty"`
	CreatedAt string          `json:"created_at"`
}

// CreateAction inserts a user action.
func CreateAction(ctx context.Context, db *sql.DB, userID int64, itemID *int64, action string, data json.RawMessage) (int64, error) {
	res, err := db.ExecContext(ctx, `
INSERT INTO user_actions (user_id, item_id, action, data) VALUES (?, ?, ?, ?)
`, userID, itemID, action, string(data))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// DeleteAction removes a user action by ID, scoped to the user.
func DeleteAction(ctx context.Context, db *sql.DB, userID, actionID int64) error {
	_, err := db.ExecContext(ctx, `DELETE FROM user_actions WHERE id = ? AND user_id = ?`, actionID, userID)
	return err
}

// GetItemActions returns all actions for a user on a specific item.
func GetItemActions(ctx context.Context, db *sql.DB, userID, itemID int64) ([]UserAction, error) {
	rows, err := db.QueryContext(ctx, `
SELECT id, user_id, item_id, action, COALESCE(data,''), created_at
FROM user_actions WHERE user_id = ? AND item_id = ?
ORDER BY created_at
`, userID, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanActions(rows)
}

// IsItemSaved checks if a user has saved a specific item.
func IsItemSaved(ctx context.Context, db *sql.DB, userID, itemID int64) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM user_actions WHERE user_id = ? AND item_id = ? AND action = 'save'
`, userID, itemID).Scan(&count)
	return count > 0, err
}

// SavedItem is a saved article with its item data.
type SavedItem struct {
	ActionID  int64
	SavedAt   string
	ID        int64
	Title     string
	TitleTrans string
	Summary   string
	URL       string
	FeedURL   string
	Urgency   int
	CreatedAt string
}

// ListSavedItems returns all saved items for a user, newest first.
func ListSavedItems(ctx context.Context, db *sql.DB, userID int64) ([]SavedItem, error) {
	rows, err := db.QueryContext(ctx, `
SELECT ua.id, ua.created_at,
       i.id, i.title, COALESCE(i.title_translated,''), COALESCE(i.summary,''),
       COALESCE(i.url,''), COALESCE(i.feed_url,''), i.urgency, i.created_at
FROM user_actions ua
JOIN items i ON i.id = ua.item_id
WHERE ua.user_id = ? AND ua.action = 'save'
ORDER BY ua.created_at DESC
`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SavedItem
	for rows.Next() {
		var s SavedItem
		if err := rows.Scan(&s.ActionID, &s.SavedAt,
			&s.ID, &s.Title, &s.TitleTrans, &s.Summary,
			&s.URL, &s.FeedURL, &s.Urgency, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// HighlightWithItem is a highlight action with its parent article info.
type HighlightWithItem struct {
	ActionID  int64
	ItemID    int64
	ItemTitle string
	Data      json.RawMessage
	CreatedAt string
}

// ListHighlights returns all highlights for a user, grouped by recency.
func ListHighlights(ctx context.Context, db *sql.DB, userID int64) ([]HighlightWithItem, error) {
	rows, err := db.QueryContext(ctx, `
SELECT ua.id, ua.item_id, COALESCE(i.title_translated, i.title),
       COALESCE(ua.data,'{}'), ua.created_at
FROM user_actions ua
JOIN items i ON i.id = ua.item_id
WHERE ua.user_id = ? AND ua.action = 'highlight'
ORDER BY ua.created_at DESC
`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HighlightWithItem
	for rows.Next() {
		var h HighlightWithItem
		if err := rows.Scan(&h.ActionID, &h.ItemID, &h.ItemTitle, &h.Data, &h.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// NoteWithItem is a note action with its parent article info.
type NoteWithItem struct {
	ActionID  int64
	ItemID    int64
	ItemTitle string
	Data      json.RawMessage
	CreatedAt string
}

// ListNotes returns all notes for a user, newest first.
func ListNotes(ctx context.Context, db *sql.DB, userID int64) ([]NoteWithItem, error) {
	rows, err := db.QueryContext(ctx, `
SELECT ua.id, ua.item_id, COALESCE(i.title_translated, i.title),
       COALESCE(ua.data,'{}'), ua.created_at
FROM user_actions ua
JOIN items i ON i.id = ua.item_id
WHERE ua.user_id = ? AND ua.action = 'note'
ORDER BY ua.created_at DESC
`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NoteWithItem
	for rows.Next() {
		var n NoteWithItem
		if err := rows.Scan(&n.ActionID, &n.ItemID, &n.ItemTitle, &n.Data, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ListHiddenFeeds returns all feed URLs a user has hidden.
func ListHiddenFeeds(ctx context.Context, db *sql.DB, userID int64) ([]UserAction, error) {
	rows, err := db.QueryContext(ctx, `
SELECT id, user_id, item_id, action, COALESCE(data,''), created_at
FROM user_actions WHERE user_id = ? AND action = 'hide_feed'
ORDER BY created_at DESC
`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanActions(rows)
}

// HiddenFeedURLs returns just the feed URLs a user has hidden.
func HiddenFeedURLs(ctx context.Context, db *sql.DB, userID int64) ([]string, error) {
	actions, err := ListHiddenFeeds(ctx, db, userID)
	if err != nil {
		return nil, err
	}
	var urls []string
	for _, a := range actions {
		var d struct {
			FeedURL string `json:"feed_url"`
		}
		if err := json.Unmarshal(a.Data, &d); err == nil && d.FeedURL != "" {
			urls = append(urls, d.FeedURL)
		}
	}
	return urls, nil
}

func scanActions(rows *sql.Rows) ([]UserAction, error) {
	var out []UserAction
	for rows.Next() {
		var a UserAction
		var dataStr string
		var itemID sql.NullInt64
		if err := rows.Scan(&a.ID, &a.UserID, &itemID, &a.Action, &dataStr, &a.CreatedAt); err != nil {
			return nil, err
		}
		if itemID.Valid {
			a.ItemID = &itemID.Int64
		}
		if dataStr != "" {
			a.Data = json.RawMessage(dataStr)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// UsernameExists checks if a username is already taken.
func UsernameExists(ctx context.Context, db *sql.DB, username string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE username = ?`, username).Scan(&count)
	return count > 0, err
}
