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

	data, vocab, rows, cols, err := buildTFIDFMatrix(docs, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rows != 2 {
		t.Fatalf("expected 2 rows, got %d", rows)
	}
	if cols == 0 {
		t.Fatalf("expected non-zero vocab size")
	}
	if len(vocab) != cols {
		t.Fatalf("vocab length %d does not match cols %d", len(vocab), cols)
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

func TestSummarizeClusters_TopKeywordsAndCentralKeys(t *testing.T) {
	docs := []mapDocument{
		{Key: "A-1", Text: "alpha alpha beta"},
		{Key: "A-2", Text: "alpha beta beta"},
		{Key: "B-1", Text: "gamma delta delta"},
		{Key: "B-2", Text: "gamma delta epsilon"},
	}

	tfidf, vocab, rows, cols, err := buildTFIDFMatrix(docs, 100)
	if err != nil {
		t.Fatalf("build matrix: %v", err)
	}
	if rows != 4 || cols == 0 {
		t.Fatalf("unexpected matrix shape: rows=%d cols=%d", rows, cols)
	}

	// A-cluster around (0,0), B-cluster around (10,10) — straightforward 2D
	// layout to make centroid math obvious.
	coords := [][2]float64{
		{0, 0}, {0.1, 0.1},
		{10, 10}, {10.1, 10.1},
	}
	assignments := []int{0, 0, 1, 1}

	clusters := summarizeClusters(docs, tfidf, vocab, rows, cols, assignments, coords)
	if len(clusters) != 2 {
		t.Fatalf("expected 2 clusters, got %d", len(clusters))
	}

	for _, c := range clusters {
		if c.Size != 2 {
			t.Fatalf("expected cluster %d to have size 2, got %d", c.ID, c.Size)
		}
		if len(c.TopKeywords) == 0 {
			t.Fatalf("expected non-empty top keywords for cluster %d", c.ID)
		}
		if len(c.CentralKeys) == 0 {
			t.Fatalf("expected at least one central key for cluster %d", c.ID)
		}
	}

	// Cluster A's keywords should contain alpha or beta, and not gamma/delta.
	hasAlphaOrBeta := false
	for _, kw := range clusters[0].TopKeywords {
		if kw == "alpha" || kw == "beta" {
			hasAlphaOrBeta = true
		}
		if kw == "gamma" || kw == "delta" {
			t.Fatalf("unexpected keyword %q leaked into cluster 0", kw)
		}
	}
	if !hasAlphaOrBeta {
		t.Fatalf("cluster 0 should surface alpha/beta keywords, got %v", clusters[0].TopKeywords)
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
