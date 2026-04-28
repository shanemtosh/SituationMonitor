package httpserver

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"situationmonitor/internal/store"
)

//go:embed alpha.html
var alphaHTML string

var alphaTmpl = template.Must(template.New("alpha").Parse(alphaHTML))

//go:embed alpha_landing.html
var alphaLandingHTML string

var alphaLandingTmpl = template.Must(template.New("alphaLanding").Parse(alphaLandingHTML))

var validDomains = map[string]string{
	"geopolitics":    "Geopolitics",
	"macro":          "Macro & Monetary Policy",
	"semiconductors": "Semiconductors & Technology",
	"energy":         "Energy",
}

// mountAlphaRoutes registers multi-domain alpha API endpoints.
func mountAlphaRoutes(mux *http.ServeMux, db *sql.DB) {
	// Constraints
	mux.HandleFunc("GET /api/constraints", handleListConstraints(db))
	mux.HandleFunc("POST /api/constraints", handleCreateConstraint(db))
	mux.HandleFunc("GET /api/constraints/{id}", handleGetConstraint(db))
	mux.HandleFunc("PUT /api/constraints/{id}", handleUpdateConstraint(db))
	mux.HandleFunc("DELETE /api/constraints/{id}", handleDeleteConstraint(db))

	// Net Assessments
	mux.HandleFunc("GET /api/assessments", handleListAssessments(db))
	mux.HandleFunc("POST /api/assessments", handleCreateAssessment(db))
	mux.HandleFunc("GET /api/assessments/{id}", handleGetAssessment(db))
	mux.HandleFunc("PUT /api/assessments/{id}", handleUpdateAssessment(db))
	mux.HandleFunc("DELETE /api/assessments/{id}", handleDeleteAssessment(db))
	mux.HandleFunc("POST /api/assessments/{id}/update-probability", handleUpdateProbability(db))
	mux.HandleFunc("POST /api/assessments/{id}/constraints", handleLinkConstraint(db))
	mux.HandleFunc("DELETE /api/assessments/{aid}/constraints/{cid}", handleUnlinkConstraint(db))

	// Calendar
	mux.HandleFunc("GET /api/calendar", handleListCalendar(db))
	mux.HandleFunc("POST /api/calendar", handleCreateCalendarEvent(db))
	mux.HandleFunc("GET /api/calendar/{id}", handleGetCalendarEvent(db))
	mux.HandleFunc("PUT /api/calendar/{id}", handleUpdateCalendarEvent(db))
	mux.HandleFunc("DELETE /api/calendar/{id}", handleDeleteCalendarEvent(db))

	// Data Streams
	mux.HandleFunc("GET /api/data-streams", handleListDataStreams(db))
	mux.HandleFunc("POST /api/data-streams", handleCreateDataStream(db))
	mux.HandleFunc("PUT /api/data-streams/{id}", handleUpdateDataStream(db))
	mux.HandleFunc("DELETE /api/data-streams/{id}", handleDeleteDataStream(db))

	// Composite
	mux.HandleFunc("GET /api/alpha/dashboard", handleAlphaDashboardAPI(db))
	mux.HandleFunc("GET /api/alpha/domains", handleAlphaDomainsAPI(db))

	// UI pages
	mux.HandleFunc("GET /alpha", handleAlphaLandingPage(db))
	mux.HandleFunc("GET /alpha/digests", handleDigestsIndex(db))
	mux.HandleFunc("GET /alpha/digests/{week}", handleDigestPage(db))
	mux.HandleFunc("GET /alpha/{domain}", handleAlphaDomainPage(db))
}

// --- Constraints ---

func handleListConstraints(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var sitID *int64
		if v := q.Get("situation_id"); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				sitID = &n
			}
		}
		rows, err := store.ListConstraints(r.Context(), db, sitID, q.Get("domain"), q.Get("type"), q.Get("region"), q.Get("status"))
		if err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		writeJSON(w, rows)
	}
}

func handleCreateConstraint(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			SituationID *int64 `json:"situation_id"`
			Domain      string `json:"domain"`
			Region      string `json:"region"`
			Type        string `json:"type"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Mutability  string `json:"mutability"`
			Direction   string `json:"direction"`
			Evidence    string `json:"evidence"`
			DataStreams string `json:"data_streams"`
			Status      string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, 400, "invalid JSON")
			return
		}
		if req.Name == "" || req.Type == "" {
			jsonErr(w, 400, "name and type are required")
			return
		}
		if req.Mutability == "" {
			req.Mutability = "medium"
		}
		if req.Direction == "" {
			req.Direction = "neutral"
		}
		if req.DataStreams == "" {
			req.DataStreams = "[]"
		}
		if req.Status == "" {
			req.Status = "active"
		}
		c := store.ConstraintRow{
			SituationID: req.SituationID, Domain: req.Domain, Region: req.Region, Type: req.Type,
			Name: req.Name, Description: req.Description, Mutability: req.Mutability,
			Direction: req.Direction, Evidence: req.Evidence, DataStreams: req.DataStreams, Status: req.Status,
		}
		id, err := store.CreateConstraint(r.Context(), db, c)
		if err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		writeJSON(w, map[string]any{"id": id, "status": "ok"})
	}
}

func handleGetConstraint(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			jsonErr(w, 400, "invalid id")
			return
		}
		c, err := store.GetConstraint(r.Context(), db, id)
		if err != nil {
			jsonErr(w, 404, "not found")
			return
		}
		writeJSON(w, c)
	}
}

func handleUpdateConstraint(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			jsonErr(w, 400, "invalid id")
			return
		}
		existing, err := store.GetConstraint(r.Context(), db, id)
		if err != nil {
			jsonErr(w, 404, "not found")
			return
		}
		var req struct {
			SituationID *int64  `json:"situation_id"`
			Region      *string `json:"region"`
			Type        *string `json:"type"`
			Name        *string `json:"name"`
			Description *string `json:"description"`
			Mutability  *string `json:"mutability"`
			Direction   *string `json:"direction"`
			Evidence    *string `json:"evidence"`
			DataStreams *string `json:"data_streams"`
			Status      *string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, 400, "invalid JSON")
			return
		}
		if req.SituationID != nil {
			existing.SituationID = req.SituationID
		}
		if req.Region != nil {
			existing.Region = *req.Region
		}
		if req.Type != nil {
			existing.Type = *req.Type
		}
		if req.Name != nil {
			existing.Name = *req.Name
		}
		if req.Description != nil {
			existing.Description = *req.Description
		}
		if req.Mutability != nil {
			existing.Mutability = *req.Mutability
		}
		if req.Direction != nil {
			existing.Direction = *req.Direction
		}
		if req.Evidence != nil {
			existing.Evidence = *req.Evidence
		}
		if req.DataStreams != nil {
			existing.DataStreams = *req.DataStreams
		}
		if req.Status != nil {
			existing.Status = *req.Status
		}
		if err := store.UpdateConstraint(r.Context(), db, existing); err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		jsonOK(w, "updated")
	}
}

func handleDeleteConstraint(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			jsonErr(w, 400, "invalid id")
			return
		}
		if err := store.DeleteConstraint(r.Context(), db, id); err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		jsonOK(w, "deleted")
	}
}

// --- Net Assessments ---

func handleListAssessments(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var sitID *int64
		if v := q.Get("situation_id"); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				sitID = &n
			}
		}
		rows, err := store.ListAssessments(r.Context(), db, sitID, q.Get("domain"), q.Get("lens"), q.Get("status"))
		if err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		writeJSON(w, rows)
	}
}

func handleCreateAssessment(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			SituationID            int64    `json:"situation_id"`
			Domain                 string   `json:"domain"`
			Lens                   string   `json:"lens"`
			Title                  string   `json:"title"`
			Summary                string   `json:"summary"`
			PriorProbability       *float64 `json:"prior_probability"`
			CurrentProbability     *float64 `json:"current_probability"`
			FulcrumConstraintID    *int64   `json:"fulcrum_constraint_id"`
			BaseCase               string   `json:"base_case"`
			BullCase               string   `json:"bull_case"`
			BearCase               string   `json:"bear_case"`
			InvestmentImplications string   `json:"investment_implications"`
			Status                 string   `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, 400, "invalid JSON")
			return
		}
		if req.SituationID == 0 || req.Title == "" {
			jsonErr(w, 400, "situation_id and title are required")
			return
		}
		if req.Lens == "" {
			req.Lens = "cyclical"
		}
		if req.Status == "" {
			req.Status = "active"
		}
		// Default current to prior if not specified
		if req.CurrentProbability == nil && req.PriorProbability != nil {
			req.CurrentProbability = req.PriorProbability
		}
		a := store.AssessmentRow{
			SituationID: req.SituationID, Domain: req.Domain, Lens: req.Lens, Title: req.Title, Summary: req.Summary,
			PriorProbability: req.PriorProbability, CurrentProbability: req.CurrentProbability,
			FulcrumConstraintID: req.FulcrumConstraintID, BaseCase: req.BaseCase, BullCase: req.BullCase,
			BearCase: req.BearCase, InvestmentImplications: req.InvestmentImplications, Status: req.Status,
		}
		id, err := store.CreateAssessment(r.Context(), db, a)
		if err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		writeJSON(w, map[string]any{"id": id, "status": "ok"})
	}
}

func handleGetAssessment(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			jsonErr(w, 400, "invalid id")
			return
		}
		ctx := r.Context()
		a, err := store.GetAssessment(ctx, db, id)
		if err != nil {
			jsonErr(w, 404, "not found")
			return
		}
		// Include linked constraints and update history
		acs, _ := store.GetAssessmentConstraints(ctx, db, id)
		updates, _ := store.ListProbabilityUpdates(ctx, db, id)

		// Resolve constraint details
		type constraintDetail struct {
			store.ConstraintRow
			Weight string `json:"weight"`
			Notes  string `json:"notes"`
		}
		var constraints []constraintDetail
		for _, ac := range acs {
			c, err := store.GetConstraint(ctx, db, ac.ConstraintID)
			if err != nil {
				continue
			}
			constraints = append(constraints, constraintDetail{ConstraintRow: c, Weight: ac.Weight, Notes: ac.Notes})
		}

		writeJSON(w, map[string]any{
			"assessment":  a,
			"constraints": constraints,
			"updates":     updates,
		})
	}
}

func handleUpdateAssessment(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			jsonErr(w, 400, "invalid id")
			return
		}
		existing, err := store.GetAssessment(r.Context(), db, id)
		if err != nil {
			jsonErr(w, 404, "not found")
			return
		}
		var req struct {
			SituationID            *int64   `json:"situation_id"`
			Lens                   *string  `json:"lens"`
			Title                  *string  `json:"title"`
			Summary                *string  `json:"summary"`
			PriorProbability       *float64 `json:"prior_probability"`
			CurrentProbability     *float64 `json:"current_probability"`
			FulcrumConstraintID    *int64   `json:"fulcrum_constraint_id"`
			BaseCase               *string  `json:"base_case"`
			BullCase               *string  `json:"bull_case"`
			BearCase               *string  `json:"bear_case"`
			InvestmentImplications *string  `json:"investment_implications"`
			Status                 *string  `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, 400, "invalid JSON")
			return
		}
		if req.SituationID != nil {
			existing.SituationID = *req.SituationID
		}
		if req.Lens != nil {
			existing.Lens = *req.Lens
		}
		if req.Title != nil {
			existing.Title = *req.Title
		}
		if req.Summary != nil {
			existing.Summary = *req.Summary
		}
		if req.PriorProbability != nil {
			existing.PriorProbability = req.PriorProbability
		}
		if req.CurrentProbability != nil {
			existing.CurrentProbability = req.CurrentProbability
		}
		if req.FulcrumConstraintID != nil {
			existing.FulcrumConstraintID = req.FulcrumConstraintID
		}
		if req.BaseCase != nil {
			existing.BaseCase = *req.BaseCase
		}
		if req.BullCase != nil {
			existing.BullCase = *req.BullCase
		}
		if req.BearCase != nil {
			existing.BearCase = *req.BearCase
		}
		if req.InvestmentImplications != nil {
			existing.InvestmentImplications = *req.InvestmentImplications
		}
		if req.Status != nil {
			existing.Status = *req.Status
		}
		if err := store.UpdateAssessment(r.Context(), db, existing); err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		jsonOK(w, "updated")
	}
}

func handleDeleteAssessment(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			jsonErr(w, 400, "invalid id")
			return
		}
		if err := store.DeleteAssessment(r.Context(), db, id); err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		jsonOK(w, "deleted")
	}
}

func handleUpdateProbability(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			jsonErr(w, 400, "invalid id")
			return
		}
		var req struct {
			Prior        float64 `json:"prior"`
			Posterior    float64 `json:"posterior"`
			Evidence     string  `json:"evidence"`
			SourceItemID *int64  `json:"source_item_id"`
			ConstraintID *int64  `json:"constraint_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, 400, "invalid JSON")
			return
		}
		if req.Evidence == "" {
			jsonErr(w, 400, "evidence is required")
			return
		}
		u := store.ProbabilityUpdateRow{
			AssessmentID: id,
			Prior:        req.Prior,
			Posterior:    req.Posterior,
			Evidence:     req.Evidence,
			SourceItemID: req.SourceItemID,
			ConstraintID: req.ConstraintID,
		}
		uid, err := store.CreateProbabilityUpdate(r.Context(), db, u)
		if err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		writeJSON(w, map[string]any{"id": uid, "status": "ok"})
	}
}

func handleLinkConstraint(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			jsonErr(w, 400, "invalid id")
			return
		}
		var req struct {
			ConstraintID int64  `json:"constraint_id"`
			Weight       string `json:"weight"`
			Notes        string `json:"notes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ConstraintID == 0 {
			jsonErr(w, 400, "need constraint_id")
			return
		}
		if req.Weight == "" {
			req.Weight = "medium"
		}
		if err := store.LinkAssessmentConstraint(r.Context(), db, id, req.ConstraintID, req.Weight, req.Notes); err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		jsonOK(w, "linked")
	}
}

func handleUnlinkConstraint(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		aid, err := strconv.ParseInt(r.PathValue("aid"), 10, 64)
		if err != nil {
			jsonErr(w, 400, "invalid assessment id")
			return
		}
		cid, err := strconv.ParseInt(r.PathValue("cid"), 10, 64)
		if err != nil {
			jsonErr(w, 400, "invalid constraint id")
			return
		}
		if err := store.UnlinkAssessmentConstraint(r.Context(), db, aid, cid); err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		jsonOK(w, "unlinked")
	}
}

// --- Calendar ---

func handleListCalendar(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		rows, err := store.ListCalendarEvents(r.Context(), db, q.Get("from"), q.Get("to"), q.Get("domain"), q.Get("region"), q.Get("status"))
		if err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		writeJSON(w, rows)
	}
}

func handleCreateCalendarEvent(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			EventDate       string `json:"event_date"`
			Title           string `json:"title"`
			Description     string `json:"description"`
			Domain          string `json:"domain"`
			Region          string `json:"region"`
			EventType       string `json:"event_type"`
			MarketRelevance string `json:"market_relevance"`
			AssessmentID    *int64 `json:"assessment_id"`
			Status          string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, 400, "invalid JSON")
			return
		}
		if req.EventDate == "" || req.Title == "" {
			jsonErr(w, 400, "event_date and title are required")
			return
		}
		if req.EventType == "" {
			req.EventType = "other"
		}
		if req.MarketRelevance == "" {
			req.MarketRelevance = "medium"
		}
		if req.Status == "" {
			req.Status = "upcoming"
		}
		e := store.CalendarEventRow{
			EventDate: req.EventDate, Title: req.Title, Description: req.Description,
			Domain: req.Domain, Region: req.Region, EventType: req.EventType, MarketRelevance: req.MarketRelevance,
			AssessmentID: req.AssessmentID, Status: req.Status,
		}
		id, err := store.CreateCalendarEvent(r.Context(), db, e)
		if err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		writeJSON(w, map[string]any{"id": id, "status": "ok"})
	}
}

func handleGetCalendarEvent(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			jsonErr(w, 400, "invalid id")
			return
		}
		e, err := store.GetCalendarEvent(r.Context(), db, id)
		if err != nil {
			jsonErr(w, 404, "not found")
			return
		}
		writeJSON(w, e)
	}
}

func handleUpdateCalendarEvent(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			jsonErr(w, 400, "invalid id")
			return
		}
		existing, err := store.GetCalendarEvent(r.Context(), db, id)
		if err != nil {
			jsonErr(w, 404, "not found")
			return
		}
		var req struct {
			EventDate       *string `json:"event_date"`
			Title           *string `json:"title"`
			Description     *string `json:"description"`
			Region          *string `json:"region"`
			EventType       *string `json:"event_type"`
			MarketRelevance *string `json:"market_relevance"`
			AssessmentID    *int64  `json:"assessment_id"`
			Status          *string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, 400, "invalid JSON")
			return
		}
		if req.EventDate != nil {
			existing.EventDate = *req.EventDate
		}
		if req.Title != nil {
			existing.Title = *req.Title
		}
		if req.Description != nil {
			existing.Description = *req.Description
		}
		if req.Region != nil {
			existing.Region = *req.Region
		}
		if req.EventType != nil {
			existing.EventType = *req.EventType
		}
		if req.MarketRelevance != nil {
			existing.MarketRelevance = *req.MarketRelevance
		}
		if req.AssessmentID != nil {
			existing.AssessmentID = req.AssessmentID
		}
		if req.Status != nil {
			existing.Status = *req.Status
		}
		if err := store.UpdateCalendarEvent(r.Context(), db, existing); err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		jsonOK(w, "updated")
	}
}

func handleDeleteCalendarEvent(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			jsonErr(w, 400, "invalid id")
			return
		}
		if err := store.DeleteCalendarEvent(r.Context(), db, id); err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		jsonOK(w, "deleted")
	}
}

// --- Data Streams ---

func handleListDataStreams(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var cID *int64
		if v := r.URL.Query().Get("constraint_id"); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				cID = &n
			}
		}
		rows, err := store.ListDataStreams(r.Context(), db, cID)
		if err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		writeJSON(w, rows)
	}
}

func handleCreateDataStream(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ConstraintID  int64  `json:"constraint_id"`
			Name          string `json:"name"`
			Description   string `json:"description"`
			SourceType    string `json:"source_type"`
			SourceConfig  string `json:"source_config"`
			ThresholdNote string `json:"threshold_note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, 400, "invalid JSON")
			return
		}
		if req.ConstraintID == 0 || req.Name == "" {
			jsonErr(w, 400, "constraint_id and name are required")
			return
		}
		if req.SourceType == "" {
			req.SourceType = "manual"
		}
		if req.SourceConfig == "" {
			req.SourceConfig = "{}"
		}
		ds := store.DataStreamRow{
			ConstraintID: req.ConstraintID, Name: req.Name, Description: req.Description,
			SourceType: req.SourceType, SourceConfig: req.SourceConfig, ThresholdNote: req.ThresholdNote,
		}
		id, err := store.CreateDataStream(r.Context(), db, ds)
		if err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		writeJSON(w, map[string]any{"id": id, "status": "ok"})
	}
}

func handleUpdateDataStream(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			jsonErr(w, 400, "invalid id")
			return
		}
		existing, err := store.GetDataStream(r.Context(), db, id)
		if err != nil {
			jsonErr(w, 404, "not found")
			return
		}
		var req struct {
			ConstraintID  *int64  `json:"constraint_id"`
			Name          *string `json:"name"`
			Description   *string `json:"description"`
			SourceType    *string `json:"source_type"`
			SourceConfig  *string `json:"source_config"`
			LastValue     *string `json:"last_value"`
			LastCheckedAt *string `json:"last_checked_at"`
			ThresholdNote *string `json:"threshold_note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, 400, "invalid JSON")
			return
		}
		if req.ConstraintID != nil {
			existing.ConstraintID = *req.ConstraintID
		}
		if req.Name != nil {
			existing.Name = *req.Name
		}
		if req.Description != nil {
			existing.Description = *req.Description
		}
		if req.SourceType != nil {
			existing.SourceType = *req.SourceType
		}
		if req.SourceConfig != nil {
			existing.SourceConfig = *req.SourceConfig
		}
		if req.LastValue != nil {
			existing.LastValue = req.LastValue
		}
		if req.LastCheckedAt != nil {
			existing.LastCheckedAt = req.LastCheckedAt
		}
		if req.ThresholdNote != nil {
			existing.ThresholdNote = *req.ThresholdNote
		}
		if err := store.UpdateDataStream(r.Context(), db, existing); err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		jsonOK(w, "updated")
	}
}

func handleDeleteDataStream(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			jsonErr(w, 400, "invalid id")
			return
		}
		if err := store.DeleteDataStream(r.Context(), db, id); err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		jsonOK(w, "deleted")
	}
}

// --- Composite ---

func handleAlphaDashboardAPI(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		domain := r.URL.Query().Get("domain")
		assessments, _ := store.ListAssessments(ctx, db, nil, domain, "", "active")

		now := time.Now().UTC().Format("2006-01-02")
		future := time.Now().UTC().AddDate(0, 0, 30).Format("2006-01-02")
		calendar, _ := store.ListCalendarEvents(ctx, db, now, future, domain, "", "upcoming")

		var recentUpdates []store.ProbabilityUpdateRow
		for _, a := range assessments {
			ups, _ := store.ListProbabilityUpdates(ctx, db, a.ID)
			if len(ups) > 3 {
				ups = ups[:3]
			}
			recentUpdates = append(recentUpdates, ups...)
		}

		writeJSON(w, map[string]any{
			"assessments":    assessments,
			"calendar":       calendar,
			"recent_updates": recentUpdates,
		})
	}
}

func handleAlphaDomainsAPI(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		summaries, err := store.ListDomainSummaries(r.Context(), db)
		if err != nil {
			jsonErr(w, 500, err.Error())
			return
		}
		// Include all configured domains even if they have no data yet
		byDomain := make(map[string]store.DomainSummaryRow)
		for _, s := range summaries {
			byDomain[s.Domain] = s
		}
		var out []map[string]any
		for slug, label := range validDomains {
			s := byDomain[slug]
			out = append(out, map[string]any{
				"domain":             slug,
				"label":              label,
				"active_assessments": s.ActiveAssessments,
				"active_constraints": s.ActiveConstraints,
				"upcoming_events":    s.UpcomingEvents,
				"last_updated":       s.LastUpdated,
			})
		}
		writeJSON(w, out)
	}
}

func handleAlphaLandingPage(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = alphaLandingTmpl.Execute(w, nil)
	}
}

func handleAlphaDomainPage(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		domain := r.PathValue("domain")
		label, ok := validDomains[domain]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = alphaTmpl.Execute(w, map[string]string{
			"Domain":      domain,
			"DomainLabel": label,
		})
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}
