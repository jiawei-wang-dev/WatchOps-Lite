package embedding

import "context"

type Provider interface {
	Embed(context.Context, []string) ([][]float32, error)
	Dimension() int
}
