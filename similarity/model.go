package similarity

import (
	"fmt"
	"math"

	"gonum.org/v1/gonum/mat"
)

type SimilarityModel struct {
	weights   *mat.Dense
	inputSize int
}

func NewSimilarityModel(inputSize int) *SimilarityModel {
	return &SimilarityModel{
		weights:   mat.NewDense(inputSize, 1, make([]float64, inputSize)), // Initialize with zeros
		inputSize: inputSize,
	}
}

func (m *SimilarityModel) Train(features [][]float64, similarities []float64) error {
	if len(features) == 0 || len(similarities) == 0 {
		return nil // Nothing to train on
	}

	if len(features[0]) != m.inputSize {
		return fmt.Errorf("feature dimension mismatch: got %d, want %d", len(features[0]), m.inputSize)
	}

	// Convert features to matrix
	X := mat.NewDense(len(features), m.inputSize, nil)
	for i, feature := range features {
		if len(feature) != m.inputSize {
			return fmt.Errorf("inconsistent feature dimensions at index %d", i)
		}
		X.SetRow(i, feature)
	}

	// Convert similarities to vector
	y := mat.NewVecDense(len(similarities), similarities)

	// Solve using normal equations: w = (X^T X)^(-1) X^T y
	var XtX mat.Dense
	XtX.Mul(X.T(), X)

	var XtXInv mat.Dense
	if err := XtXInv.Inverse(&XtX); err != nil {
		return fmt.Errorf("failed to compute inverse: %v", err)
	}

	var Xty mat.Dense
	Xty.Mul(X.T(), y)

	var w mat.Dense
	w.Mul(&XtXInv, &Xty)

	// Update weights
	m.weights.Copy(&w)
	return nil
}

func (m *SimilarityModel) Predict(features []float64) float64 {
	if len(features) != m.inputSize {
		return 0 // Return default value for invalid input
	}

	x := mat.NewVecDense(m.inputSize, features)
	// Multiply x (1 x n) by weights (n x 1) to get a scalar
	var score float64
	for i := 0; i < m.inputSize; i++ {
		score += x.AtVec(i) * m.weights.At(i, 0)
	}

	// Normalize to [0,1] using sigmoid
	return sigmoid(score)
}

func sigmoid(x float64) float64 {
	return 1.0 / (1.0 + math.Exp(-x))
}
