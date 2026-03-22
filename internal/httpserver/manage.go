package httpserver

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"situationmonitor/internal/store"
)

// mountManageRoutes registers management endpoints for the knowledge graph.
func mountManageRoutes(mux *http.ServeMux, db *sql.DB) {
	mux.HandleFunc("POST /api/entities/merge", handleMergeEntities(db))
	mux.HandleFunc("POST /api/entities/{id}/rename", handleRenameEntity(db))
	mux.HandleFunc("DELETE /api/entities/{id}", handleDeleteEntity(db))
	mux.HandleFunc("GET /api/entities", handleListEntities(db))

	mux.HandleFunc("POST /api/situations/{id}/parent", handleSetParent(db))
	mux.HandleFunc("POST /api/situations/{id}/rename", handleRenameSituation(db))
	mux.HandleFunc("POST /api/situations/{id}/status", handleSetStatus(db))
	mux.HandleFunc("POST /api/situations/merge", handleMergeSituations(db))
	mux.HandleFunc("DELETE /api/situations/{id}", handleDeleteSituation(db))
}

func jsonOK(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": msg})
}

func jsonErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func handleMergeEntities(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			FromID int64 `json:"from_id"`
			ToID   int64 `json:"to_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.FromID == 0 || req.ToID == 0 {
			jsonErr(w, 400, "need from_id and to_id")
			return
		}
		if err := store.MergeEntities(r.Context(), db, req.FromID, req.ToID); err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		jsonOK(w, fmt.Sprintf("merged entity %d into %d", req.FromID, req.ToID))
	}
}

func handleRenameEntity(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			jsonErr(w, 400, "invalid id")
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
			jsonErr(w, 400, "need name")
			return
		}
		if err := store.RenameEntity(r.Context(), db, id, req.Name); err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		jsonOK(w, fmt.Sprintf("renamed entity %d to %q", id, req.Name))
	}
}

func handleDeleteEntity(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			jsonErr(w, 400, "invalid id")
			return
		}
		if err := store.DeleteEntity(r.Context(), db, id); err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		jsonOK(w, fmt.Sprintf("deleted entity %d", id))
	}
}

func handleListEntities(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		kind := r.URL.Query().Get("kind")
		limit := 50
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
				limit = n
			}
		}

		entities, err := store.ListTopEntities(r.Context(), db, kind, limit)
		if err != nil {
			jsonErr(w, 500, err.Error())
			return
		}

		type entJSON struct {
			ID        int64  `json:"id"`
			Name      string `json:"name"`
			Kind      string `json:"kind"`
			ItemCount int    `json:"item_count"`
			FirstSeen string `json:"first_seen"`
			LastSeen  string `json:"last_seen"`
		}
		out := make([]entJSON, 0, len(entities))
		for _, e := range entities {
			out = append(out, entJSON{
				ID: e.ID, Name: e.Name, Kind: e.Kind, ItemCount: e.ItemCount,
				FirstSeen: e.FirstSeen, LastSeen: e.LastSeen,
			})
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(out)
	}
}

func handleSetParent(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			jsonErr(w, 400, "invalid id")
			return
		}
		var req struct {
			ParentID *int64 `json:"parent_id"` // null to unset
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, 400, "need parent_id (or null to unset)")
			return
		}
		if req.ParentID != nil {
			err = store.SetSituationParent(r.Context(), db, id, *req.ParentID)
		} else {
			_, err = db.ExecContext(r.Context(), `UPDATE situations SET parent_id = NULL WHERE id = ?`, id)
		}
		if err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		jsonOK(w, fmt.Sprintf("set parent of situation %d", id))
	}
}

func handleRenameSituation(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			jsonErr(w, 400, "invalid id")
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
			jsonErr(w, 400, "need name")
			return
		}
		if err := store.RenameSituation(r.Context(), db, id, req.Name); err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		jsonOK(w, fmt.Sprintf("renamed situation %d to %q", id, req.Name))
	}
}

func handleSetStatus(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			jsonErr(w, 400, "invalid id")
			return
		}
		var req struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Status == "" {
			jsonErr(w, 400, "need status (active/resolved/watching)")
			return
		}
		if err := store.SetSituationStatus(r.Context(), db, id, req.Status); err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		jsonOK(w, fmt.Sprintf("set situation %d status to %q", id, req.Status))
	}
}

func handleMergeSituations(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			FromID int64 `json:"from_id"`
			ToID   int64 `json:"to_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.FromID == 0 || req.ToID == 0 {
			jsonErr(w, 400, "need from_id and to_id")
			return
		}
		if err := store.MergeSituations(r.Context(), db, req.FromID, req.ToID); err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		jsonOK(w, fmt.Sprintf("merged situation %d into %d", req.FromID, req.ToID))
	}
}

func handleDeleteSituation(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			jsonErr(w, 400, "invalid id")
			return
		}
		if err := store.DeleteSituation(r.Context(), db, id); err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		jsonOK(w, fmt.Sprintf("deleted situation %d", id))
	}
}
