package httpserver

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"html/template"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"situationmonitor/internal/store"
)

type contextKey string

const userKey contextKey = "user"

// UserFromContext returns the logged-in user, or nil if anonymous.
func UserFromContext(ctx context.Context) *store.User {
	u, _ := ctx.Value(userKey).(*store.User)
	return u
}

// withUser is middleware that reads the session cookie and injects *User into context.
func withUser(db *sql.DB, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("sm_session")
		if err == nil && c.Value != "" {
			if u, err := store.GetSession(r.Context(), db, c.Value); err == nil {
				r = r.WithContext(context.WithValue(r.Context(), userKey, u))
			}
		}
		next.ServeHTTP(w, r)
	})
}

//go:embed login.html
var loginHTML string

var loginTmpl = template.Must(template.New("login").Parse(loginHTML))

type loginData struct {
	Error    string
	Mode     string // "login" or "register"
	Username string
	Email    string
}

func handleLogin(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Already logged in? redirect to home
		if UserFromContext(r.Context()) != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		mode := r.URL.Query().Get("mode")
		if mode != "register" {
			mode = "login"
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = loginTmpl.Execute(w, loginData{Mode: mode})
	}
}

var emailRe = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

func handleLoginPost(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", 400)
			return
		}
		mode := r.FormValue("mode")
		username := strings.TrimSpace(r.FormValue("username"))
		password := r.FormValue("password")
		email := strings.TrimSpace(r.FormValue("email"))

		render := func(errMsg string) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_ = loginTmpl.Execute(w, loginData{Error: errMsg, Mode: mode, Username: username, Email: email})
		}

		if username == "" || password == "" {
			render("Username and password are required.")
			return
		}
		if len(password) < 8 {
			render("Password must be at least 8 characters.")
			return
		}

		ctx := r.Context()

		if mode == "register" {
			if email == "" || !emailRe.MatchString(email) {
				render("A valid email is required.")
				return
			}
			if len(username) < 3 || len(username) > 30 {
				render("Username must be 3-30 characters.")
				return
			}
			exists, _ := store.UsernameExists(ctx, db, username)
			if exists {
				render("Username is already taken.")
				return
			}
			userID, err := store.CreateUser(ctx, db, username, email, password)
			if err != nil {
				render("Registration failed. Try a different username.")
				return
			}
			token, err := store.CreateSession(ctx, db, userID)
			if err != nil {
				render("Account created but login failed. Try signing in.")
				return
			}
			setSessionCookie(w, token)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		// Login
		u, err := store.AuthenticateUser(ctx, db, username, password)
		if err != nil {
			render("Invalid username or password.")
			return
		}
		token, err := store.CreateSession(ctx, db, u.ID)
		if err != nil {
			render("Login failed. Please try again.")
			return
		}
		setSessionCookie(w, token)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func handleLogout(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("sm_session"); err == nil {
			_ = store.DeleteSession(r.Context(), db, c.Value)
		}
		http.SetCookie(w, &http.Cookie{
			Name:     "sm_session",
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "sm_session",
		Value:    token,
		Path:     "/",
		MaxAge:   30 * 24 * 60 * 60, // 30 days
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// handleActions handles POST/DELETE/GET for user actions (save, highlight, note, hide_feed).
func handleActionsAPI(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := UserFromContext(r.Context())
		if u == nil {
			jsonErr(w, 401, "login required")
			return
		}

		switch r.Method {
		case http.MethodPost:
			var req struct {
				ItemID *int64          `json:"item_id"`
				Action string          `json:"action"`
				Data   json.RawMessage `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				jsonErr(w, 400, "invalid json")
				return
			}
			valid := map[string]bool{"save": true, "highlight": true, "note": true, "hide_feed": true}
			if !valid[req.Action] {
				jsonErr(w, 400, "invalid action")
				return
			}
			id, err := store.CreateAction(r.Context(), db, u.ID, req.ItemID, req.Action, req.Data)
			if err != nil {
				jsonErr(w, 500, err.Error())
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "id": id})

		case http.MethodGet:
			itemIDStr := r.URL.Query().Get("item_id")
			if itemIDStr == "" {
				jsonErr(w, 400, "item_id required")
				return
			}
			itemID, err := strconv.ParseInt(itemIDStr, 10, 64)
			if err != nil {
				jsonErr(w, 400, "invalid item_id")
				return
			}
			actions, err := store.GetItemActions(r.Context(), db, u.ID, itemID)
			if err != nil {
				jsonErr(w, 500, err.Error())
				return
			}
			if actions == nil {
				actions = []store.UserAction{}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(actions)

		default:
			jsonErr(w, 405, "method not allowed")
		}
	}
}

func handleDeleteAction(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := UserFromContext(r.Context())
		if u == nil {
			jsonErr(w, 401, "login required")
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			jsonErr(w, 400, "invalid id")
			return
		}
		if err := store.DeleteAction(r.Context(), db, u.ID, id); err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		jsonOK(w, "deleted")
	}
}
