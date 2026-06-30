package elasticsearch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	elasticsearchgo "github.com/elastic/go-elasticsearch/v9"
)

type Config struct {
	Addresses      []string
	Username       string
	Password       string
	RequestTimeout time.Duration
}

type Request struct {
	Method      string
	Path        string
	Body        io.Reader
	ContentType string
}

type Executor interface {
	Do(ctx context.Context, request Request) (*http.Response, error)
}

type Client struct {
	client         *elasticsearchgo.BaseClient
	requestTimeout time.Duration
}

func New(config Config) (*Client, error) {
	if len(config.Addresses) == 0 {
		return nil, errors.New("at least one Elasticsearch address is required")
	}
	if config.RequestTimeout <= 0 {
		return nil, errors.New("Elasticsearch request timeout must be greater than zero")
	}

	options := []elasticsearchgo.Option{
		elasticsearchgo.WithAddresses(config.Addresses...),
		elasticsearchgo.WithAutoDrainBody(),
		elasticsearchgo.WithRetry(1, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout),
	}
	if config.Username != "" || config.Password != "" {
		options = append(options, elasticsearchgo.WithBasicAuth(config.Username, config.Password))
	}

	client, err := elasticsearchgo.NewBase(options...)
	if err != nil {
		return nil, err
	}
	return &Client{
		client:         client,
		requestTimeout: config.RequestTimeout,
	}, nil
}

func (c *Client) Do(ctx context.Context, request Request) (*http.Response, error) {
	requestContext, cancel := context.WithTimeout(ctx, c.requestTimeout)

	httpRequest, err := http.NewRequestWithContext(
		requestContext,
		request.Method,
		request.Path,
		request.Body,
	)
	if err != nil {
		cancel()
		return nil, err
	}
	if request.ContentType != "" {
		httpRequest.Header.Set("Content-Type", request.ContentType)
	}

	response, err := c.client.Perform(httpRequest)
	if err != nil {
		cancel()
		return nil, err
	}
	response.Body = &cancelReadCloser{
		ReadCloser: response.Body,
		cancel:     cancel,
	}
	return response, nil
}

func (c *Client) Close(ctx context.Context) error {
	return c.client.Close(ctx)
}

type cancelReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelReadCloser) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

var _ Executor = (*Client)(nil)
