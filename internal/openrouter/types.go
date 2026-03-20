package openrouter

// SweepResponse is the JSON contract we ask the model to return.
type SweepResponse struct {
	Stories []SweepStory `json:"stories"`
}

// SweepStory is one synthesized situation item from a sweep.
type SweepStory struct {
	Title   string   `json:"title"`
	Summary string   `json:"summary"`
	Why     string   `json:"why_it_matters"`
	Urgency int      `json:"urgency"`
	Region  string   `json:"region"`
	Tags    []string `json:"tags"`
	Sources []struct {
		URL   string `json:"url"`
		Title string `json:"title"`
		IsX   bool   `json:"is_x"`
	} `json:"sources"`
	XAngle []string `json:"x_angle"`
}
