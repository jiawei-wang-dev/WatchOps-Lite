package mcp

import (
	"context"
	"errors"
)

type Client interface {
	CallTool(
		ctx context.Context,
		tool string,
		args map[string]any,
	) (map[string]any, error)
}

type DisabledClient struct{}

func (DisabledClient) CallTool(
	context.Context,
	string,
	map[string]any,
) (map[string]any, error) {
	return nil, ErrDisabled
}

func IsUnavailable(err error) bool {
	return errors.Is(err, ErrDisabled) || errors.Is(err, ErrUnavailable)
}
