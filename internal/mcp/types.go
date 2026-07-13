package mcp

import (
	"errors"
	"fmt"
)

var (
	ErrDisabled    = errors.New("mcp client disabled")
	ErrUnavailable = errors.New("mcp server unavailable")
)

type ToolError struct {
	Code    int
	Message string
}

func (e ToolError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("%s: HTTP %d", ErrUnavailable, e.Code)
	}
	return fmt.Sprintf("%s: HTTP %d: %s", ErrUnavailable, e.Code, e.Message)
}

func (e ToolError) Unwrap() error {
	return ErrUnavailable
}
