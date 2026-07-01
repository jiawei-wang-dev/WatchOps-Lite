package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const demoMetrics = `# HELP watchops_checkout_error_rate Demonstration checkout error ratio.
# TYPE watchops_checkout_error_rate gauge
watchops_checkout_error_rate{service="checkout",environment="demo"} 0.062
# HELP watchops_checkout_p95_latency_seconds Demonstration checkout p95 latency.
# TYPE watchops_checkout_p95_latency_seconds gauge
watchops_checkout_p95_latency_seconds{service="checkout",environment="demo"} 1.8
# HELP watchops_payment_dependency_latency_seconds Demonstration payment dependency latency.
# TYPE watchops_payment_dependency_latency_seconds gauge
watchops_payment_dependency_latency_seconds{service="checkout",dependency="payment",environment="demo"} 2.4
# HELP watchops_checkout_timeout_total Demonstration checkout timeout count.
# TYPE watchops_checkout_timeout_total counter
watchops_checkout_timeout_total{service="checkout",environment="demo"} 37
`

func main() {
	healthcheck := flag.Bool("healthcheck", false, "check the local exporter health endpoint")
	flag.Parse()
	if *healthcheck {
		runHealthcheck()
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	address := os.Getenv("WATCHOPS_DEMO_METRICS_ADDRESS")
	if address == "" {
		address = ":9108"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", metricsHandler)
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"status":"ok"}`))
	})
	server := &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			logger.Error("demo metrics exporter shutdown failed", "error", err)
		}
	}()

	logger.Info("demo metrics exporter starting", "address", address)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("demo metrics exporter stopped unexpectedly", "error", err)
		os.Exit(1)
	}
}

func runHealthcheck() {
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get("http://127.0.0.1:9108/healthz")
	if err != nil {
		os.Exit(1)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		os.Exit(1)
	}
}

func metricsHandler(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = writer.Write([]byte(demoMetrics))
}
