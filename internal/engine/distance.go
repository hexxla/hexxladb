package engine

import "math"

// DistanceMetric identifies the similarity function used for embedding search.
type DistanceMetric uint8

const (
	// DistanceCosine computes cosine similarity: dot(a,b) / (‖a‖ × ‖b‖). Range [-1, 1]; higher = more similar.
	DistanceCosine DistanceMetric = 0
	// DistanceDotProduct computes the raw dot product. Assumes normalized vectors for ranking parity with cosine.
	DistanceDotProduct DistanceMetric = 1
	// DistanceL2 computes Euclidean distance. Lower = more similar; inverted to a similarity score for ranking.
	DistanceL2 DistanceMetric = 2
)

// IsValidDistanceMetric reports whether m is a recognised metric.
func IsValidDistanceMetric(m DistanceMetric) bool {
	return m <= DistanceL2
}

// Similarity returns a score where higher = more similar for any metric.
// For [DistanceL2] the result is -distance (so sorting descending still works).
func Similarity(a, b []float32, m DistanceMetric) float64 {
	switch m {
	case DistanceCosine:
		return cosineSimilarity(a, b)
	case DistanceDotProduct:
		return dotProduct(a, b)
	case DistanceL2:
		return -euclideanDistance(a, b)
	default:
		return cosineSimilarity(a, b)
	}
}

// cosineSimilarity computes dot(a,b) / (‖a‖ × ‖b‖).
func cosineSimilarity(a, b []float32) float64 {
	var dot, normA, normB float64
	for i := range a {
		ai, bi := float64(a[i]), float64(b[i])
		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}

// dotProduct computes Σ(aᵢ × bᵢ).
func dotProduct(a, b []float32) float64 {
	var sum float64
	for i := range a {
		sum += float64(a[i]) * float64(b[i])
	}
	return sum
}

// euclideanDistance computes √Σ(aᵢ - bᵢ)².
func euclideanDistance(a, b []float32) float64 {
	var sum float64
	for i := range a {
		d := float64(a[i]) - float64(b[i])
		sum += d * d
	}
	return math.Sqrt(sum)
}
