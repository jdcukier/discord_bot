package registry

import (
	"testing"
)

func TestLoad_ValidJSON(t *testing.T) {
	json := `[
		{"name":"Hip-Hop","id":"aaa","genres":["hip-hop","rap"]},
		{"name":"Electronic","id":"bbb","genres":["electronic","edm"]}
	]`
	r, err := Load(json)
	if err != nil {
		t.Fatalf("Load returned unexpected error: %v", err)
	}
	if r == nil {
		t.Fatal("Load returned nil registry")
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	_, err := Load(`not json`)
	if err == nil {
		t.Fatal("Load should return an error for invalid JSON")
	}
}

func TestLoad_EmptyArray(t *testing.T) {
	r, err := Load(`[]`)
	if err != nil {
		t.Fatalf("Load returned unexpected error: %v", err)
	}
	matches := r.Match([]string{"hip-hop"})
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches on empty registry, got %d", len(matches))
	}
}

func TestMatch_ExactKeyword(t *testing.T) {
	r, _ := Load(`[{"name":"Hip-Hop","id":"aaa","genres":["hip-hop","rap"]}]`)
	matches := r.Match([]string{"hip-hop"})
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].ID != "aaa" {
		t.Errorf("expected playlist ID aaa, got %s", matches[0].ID)
	}
}

func TestMatch_SubstringKeyword(t *testing.T) {
	// artist genre "underground hip-hop" should match keyword "hip-hop"
	r, _ := Load(`[{"name":"Hip-Hop","id":"aaa","genres":["hip-hop"]}]`)
	matches := r.Match([]string{"underground hip-hop"})
	if len(matches) != 1 {
		t.Fatalf("expected 1 match for substring, got %d", len(matches))
	}
}

func TestMatch_CaseInsensitive(t *testing.T) {
	r, _ := Load(`[{"name":"Rock","id":"ccc","genres":["rock"]}]`)
	matches := r.Match([]string{"Alt-Rock"})
	if len(matches) != 1 {
		t.Fatalf("expected 1 case-insensitive match, got %d", len(matches))
	}
}

func TestMatch_MultipleMatches(t *testing.T) {
	json := `[
		{"name":"Hip-Hop","id":"aaa","genres":["hip-hop"]},
		{"name":"Electronic","id":"bbb","genres":["electronic"]},
		{"name":"Mix","id":"ccc","genres":["hip-hop","electronic"]}
	]`
	r, _ := Load(json)
	matches := r.Match([]string{"hip-hop"})
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
}

func TestMatch_NoMatch(t *testing.T) {
	r, _ := Load(`[{"name":"Rock","id":"ccc","genres":["rock","metal"]}]`)
	matches := r.Match([]string{"jazz", "soul"})
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(matches))
	}
}

func TestMatch_NilRegistry(t *testing.T) {
	var r *Registry
	matches := r.Match([]string{"hip-hop"})
	if matches != nil {
		t.Fatalf("expected nil from nil registry, got %v", matches)
	}
}

func TestMatch_EmptyGenres(t *testing.T) {
	r, _ := Load(`[{"name":"Hip-Hop","id":"aaa","genres":["hip-hop"]}]`)
	matches := r.Match([]string{})
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches for empty genres, got %d", len(matches))
	}
}
