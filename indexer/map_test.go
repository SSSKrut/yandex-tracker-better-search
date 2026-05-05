package indexer

import (
	"math"
	"testing"
)

func TestTokenize_MixedLanguages(t *testing.T) {
	tokens := tokenize("Кнопка login-ошибка 123")
	want := map[string]bool{
		"кнопка": true,
		"login":  true,
		"ошибка": true,
		"123":    true,
	}
	for _, tok := range tokens {
		delete(want, tok)
	}
	if len(want) != 0 {
		t.Fatalf("missing tokens: %v", want)
	}
}

func TestBuildTFIDFMatrix_NormalizesRows(t *testing.T) {
	docs := []mapDocument{
		{Text: "foo bar bar"},
		{Text: "foo baz"},
	}

	data, rows, cols, err := buildTFIDFMatrix(docs, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rows != 2 {
		t.Fatalf("expected 2 rows, got %d", rows)
	}
	if cols == 0 {
		t.Fatalf("expected non-zero vocab size")
	}

	for i := 0; i < rows; i++ {
		row := data[i*cols : (i+1)*cols]
		var sum float64
		for _, v := range row {
			sum += v * v
		}
		if sum == 0 {
			continue
		}
		norm := math.Sqrt(sum)
		if math.Abs(norm-1.0) > 1e-6 {
			t.Fatalf("expected normalized row, got %f", norm)
		}
	}
}

func TestReduceWithSVD_Dimensions(t *testing.T) {
	data := []float64{
		1, 0,
		0, 1,
	}
	coords, reduced, err := reduceWithSVD(data, 2, 2, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(coords) != 2 {
		t.Fatalf("expected 2 coords, got %d", len(coords))
	}
	if len(reduced) != 2 || len(reduced[0]) != 2 {
		t.Fatalf("expected 2x2 reduced vectors")
	}
}

func TestBuildNeighbors_UsesClosest(t *testing.T) {
	docs := []mapDocument{
		{ID: "a"},
		{ID: "b"},
		{ID: "c"},
	}
	reduced := [][]float64{
		{1, 0},
		{0.9, 0.1},
		{-1, 0},
	}

	neighbors := buildNeighbors(reduced, docs, 1)
	if len(neighbors) != 3 {
		t.Fatalf("expected neighbors for all docs")
	}
	if neighbors[0][0].ID != "b" {
		t.Fatalf("expected nearest neighbor to be b, got %s", neighbors[0][0].ID)
	}
}
