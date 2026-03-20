package openrouter

import "testing"

func TestExtractJSONObject_fenced(t *testing.T) {
	in := "```json\n{\"stories\":[]}\n```"
	got := ExtractJSONObject(in)
	if got != `{"stories":[]}` {
		t.Fatalf("got %q", got)
	}
}

func TestParseSweepResponse_minimal(t *testing.T) {
	raw := `{"stories":[{"title":"Test","summary":"S","why_it_matters":"W","urgency":3,"region":"US","tags":["t"],"sources":[{"url":"https://example.com/a","title":"E","is_x":false}],"x_angle":["noise"]}]}`
	p, err := ParseSweepResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Stories) != 1 {
		t.Fatalf("stories len %d", len(p.Stories))
	}
	st := p.Stories[0]
	if st.Title != "Test" || st.Urgency != 3 {
		t.Fatalf("%+v", st)
	}
	if primaryURL(st) != "https://example.com/a" {
		t.Fatalf("url %q", primaryURL(st))
	}
}
