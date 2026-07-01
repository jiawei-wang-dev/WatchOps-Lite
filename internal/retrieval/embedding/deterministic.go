package embedding

import (
	"context"
	"crypto/sha256"
	"errors"
	"math"
)

type Deterministic struct {
	dimension int
}

func NewDeterministic(dimension int) (*Deterministic, error) {
	if dimension <= 0 {
		return nil, errors.New("embedding dimension must be greater than zero")
	}
	return &Deterministic{dimension: dimension}, nil
}

func (p *Deterministic) Dimension() int {
	return p.dimension
}

func (p *Deterministic) Embed(
	ctx context.Context,
	inputs []string,
) ([][]float32, error) {
	results := make([][]float32, 0, len(inputs))
	for _, input := range inputs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		hash := sha256.Sum256([]byte(input))
		vector := make([]float32, p.dimension)
		var magnitude float64
		for index := range vector {
			value := float32(int(hash[index%len(hash)])-127) / 128
			vector[index] = value
			magnitude += float64(value * value)
		}
		if magnitude > 0 {
			scale := float32(1 / math.Sqrt(magnitude))
			for index := range vector {
				vector[index] *= scale
			}
		}
		results = append(results, vector)
	}
	return results, nil
}
