package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"situationmonitor/internal/db"
)

func TestHealth(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	mux := http.NewServeMux()
	Mount(mux, d, t.TempDir(), ReaderConfig{})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	res, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
}
