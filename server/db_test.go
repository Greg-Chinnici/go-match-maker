package server

import (
	"context"
	"strings"
	"testing"

	"go-match-maker/glicko"
)

func TestCompareSchema(t *testing.T) {
	good := map[string]string{
		"id": "uuid", "rating": "double precision", "rd": "double precision", "volatility": "double precision",
	}
	withExtra := map[string]string{"created_at": "timestamp with time zone"}
	for k, v := range good {
		withExtra[k] = v
	}

	tests := []struct {
		name    string
		found   map[string]string
		wantErr []string // substrings the error must contain; nil means no error
	}{
		{"matches", good, nil},
		{"extra columns are fine", withExtra, nil},
		{"no table", map[string]string{}, []string{"not found", "docker compose"}},
		{
			"missing and mistyped columns",
			map[string]string{"id": "text", "rating": "double precision"},
			[]string{"column id is text, want uuid", "missing column rd", "missing column volatility"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := compareSchema(tt.found)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected an error")
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

func TestMemoryStore(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()

	if p, err := m.FetchPlayer(ctx, "missing"); p != nil || err != nil {
		t.Fatalf("missing player = %v, %v; want nil, nil", p, err)
	}

	p := glicko.EstablishedPlayer(1650, 80, 0.05, "a")
	if err := m.SavePlayers(ctx, []*glicko.Player{p}); err != nil {
		t.Fatal(err)
	}
	p.Rating = 9999 // the store keeps its own copy

	got, err := m.FetchPlayer(ctx, "a")
	if err != nil || got == nil || got.Rating != 1650 {
		t.Fatalf("FetchPlayer = %+v, %v; want rating 1650", got, err)
	}
	got.Rating = 1 // and hands out copies
	if again, _ := m.FetchPlayer(ctx, "a"); again.Rating != 1650 {
		t.Errorf("editing a fetched player changed the store: %.0f", again.Rating)
	}
}
