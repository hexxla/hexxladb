package engine

import (
	"math"
	"testing"
)

func TestCosineSimilarity_Identical(t *testing.T) {
	a := []float32{1, 2, 3}
	got := cosineSimilarity(a, a)
	if math.Abs(got-1.0) > 1e-6 {
		t.Fatalf("identical vectors: want ~1.0, got %f", got)
	}
}

func TestCosineSimilarity_Orthogonal(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{0, 1}
	got := cosineSimilarity(a, b)
	if math.Abs(got) > 1e-6 {
		t.Fatalf("orthogonal vectors: want ~0, got %f", got)
	}
}

func TestCosineSimilarity_Opposite(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{-1, -2, -3}
	got := cosineSimilarity(a, b)
	if math.Abs(got-(-1.0)) > 1e-6 {
		t.Fatalf("opposite vectors: want ~-1.0, got %f", got)
	}
}

func TestCosineSimilarity_ZeroVector(t *testing.T) {
	a := []float32{0, 0, 0}
	b := []float32{1, 2, 3}
	got := cosineSimilarity(a, b)
	if got != 0 {
		t.Fatalf("zero vector: want 0, got %f", got)
	}
}

func TestDotProduct(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{4, 5, 6}
	got := dotProduct(a, b)
	want := 32.0 // 1*4 + 2*5 + 3*6
	if math.Abs(got-want) > 1e-6 {
		t.Fatalf("dot product: want %f, got %f", want, got)
	}
}

func TestEuclideanDistance(t *testing.T) {
	a := []float32{0, 0}
	b := []float32{3, 4}
	got := euclideanDistance(a, b)
	if math.Abs(got-5.0) > 1e-6 {
		t.Fatalf("euclidean: want 5.0, got %f", got)
	}
}

func TestEuclideanDistance_Identical(t *testing.T) {
	a := []float32{1, 2, 3}
	got := euclideanDistance(a, a)
	if got != 0 {
		t.Fatalf("identical: want 0, got %f", got)
	}
}

func TestSimilarity_CosineDefault(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{1, 0}
	got := Similarity(a, b, DistanceCosine)
	if math.Abs(got-1.0) > 1e-6 {
		t.Fatalf("cosine similarity: want ~1.0, got %f", got)
	}
}

func TestSimilarity_L2_HigherIsBetter(t *testing.T) {
	a := []float32{0, 0}
	near := []float32{1, 0}
	far := []float32{10, 0}
	sNear := Similarity(a, near, DistanceL2)
	sFar := Similarity(a, far, DistanceL2)
	if sNear <= sFar {
		t.Fatalf("L2: near (%f) should score higher than far (%f)", sNear, sFar)
	}
}

func TestIsValidDistanceMetric(t *testing.T) {
	if !IsValidDistanceMetric(DistanceCosine) {
		t.Fatal("cosine should be valid")
	}
	if !IsValidDistanceMetric(DistanceL2) {
		t.Fatal("L2 should be valid")
	}
	if IsValidDistanceMetric(3) {
		t.Fatal("3 should be invalid")
	}
}

func BenchmarkCosineSimilarity_384(b *testing.B) {
	a := make([]float32, 384)
	bv := make([]float32, 384)
	for i := range a {
		a[i] = float32(i) * 0.01
		bv[i] = float32(384-i) * 0.01
	}
	for b.Loop() {
		cosineSimilarity(a, bv)
	}
}

func BenchmarkCosineSimilarity_768(b *testing.B) {
	a := make([]float32, 768)
	bv := make([]float32, 768)
	for i := range a {
		a[i] = float32(i) * 0.01
		bv[i] = float32(768-i) * 0.01
	}
	for b.Loop() {
		cosineSimilarity(a, bv)
	}
}
