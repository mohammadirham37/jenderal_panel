package website

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

// Benchmark options and limits. The generator runs inside the panel process
// and hits the site through the local Nginx listener (Host header + TLS SNI
// set to the site domain), so results reflect the real serving chain
// (Nginx -> PHP-FPM or Nginx -> Octane/FrankenPHP).
const (
	benchmarkMaxDurationSeconds = 120
	benchmarkMaxConcurrency     = 64
	benchmarkRequestTimeout     = 15 * time.Second
	benchmarkMaxErrorSamples    = 5
	// Warmup is unscored: it opens the TLS connections and lets OPcache and
	// the app boot caches settle, so measured windows are comparable.
	benchmarkWarmup             = time.Second
	benchmarkMaxBodyDrain       = 1 << 20
)

type BenchmarkOptions struct {
	DurationSeconds int    `json:"duration_seconds"`
	Concurrency     int    `json:"concurrency"`
	Path            string `json:"path"`
}

type benchmarkLatency struct {
	Avg float64 `json:"avg"`
	Min float64 `json:"min"`
	P50 float64 `json:"p50"`
	P90 float64 `json:"p90"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
	Max float64 `json:"max"`
}

type benchmarkResult struct {
	Target            string           `json:"target"`
	DurationSeconds   float64          `json:"duration_seconds"`
	TotalRequests     int64            `json:"total_requests"`
	OKRequests        int64            `json:"ok_requests"`
	FailedRequests    int64            `json:"failed_requests"`
	RequestsPerSecond float64          `json:"requests_per_second"`
	StatusCounts      map[string]int64 `json:"status_counts"`
	LatencyMS         benchmarkLatency `json:"latency_ms"`
	Errors            map[string]int64 `json:"errors,omitempty"`
}

// normalizeBenchmarkOptions clamps user input into safe bounds.
func normalizeBenchmarkOptions(opts BenchmarkOptions) BenchmarkOptions {
	if opts.DurationSeconds <= 0 {
		opts.DurationSeconds = 10
	}
	if opts.DurationSeconds > benchmarkMaxDurationSeconds {
		opts.DurationSeconds = benchmarkMaxDurationSeconds
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 10
	}
	if opts.Concurrency > benchmarkMaxConcurrency {
		opts.Concurrency = benchmarkMaxConcurrency
	}
	if opts.Path == "" {
		opts.Path = "/"
	}
	if opts.Path[0] != '/' {
		opts.Path = "/" + opts.Path
	}
	return opts
}

// benchmarkClient builds an HTTP client pinned to the local Nginx listener
// with the site's Host header and TLS SNI, so traffic never leaves the box
// and DNS/CDN do not skew the numbers.
func benchmarkClient(w model.Website) (*http.Client, *http.Request, error) {
	client := &http.Client{
		Timeout: benchmarkRequestTimeout,
		Transport: &http.Transport{
			MaxIdleConns:          benchmarkMaxConcurrency,
			MaxIdleConnsPerHost:   benchmarkMaxConcurrency,
			DisableCompression:    true,
			ResponseHeaderTimeout: benchmarkRequestTimeout,
		},
		// Measure the first hop; a redirect (e.g. http -> https) is a result.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	var rawURL string
	if w.SSLEnabled {
		dialer := &net.Dialer{Timeout: 5 * time.Second}
		client.Transport.(*http.Transport).DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, "127.0.0.1:443")
		}
		client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{
			ServerName: w.Domain,
			MinVersion: tls.VersionTLS12,
		}
		rawURL = "https://" + w.Domain
	} else {
		dialer := &net.Dialer{Timeout: 5 * time.Second}
		client.Transport.(*http.Transport).DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, "127.0.0.1:80")
		}
		rawURL = "http://" + w.Domain
	}

	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, nil, err
	}
	return client, req, nil
}

// runBenchmark drives concurrent GET requests until the deadline expires or
// ctx is canceled, and aggregates status codes and latencies. An unscored
// warmup window runs first so measured numbers start from a steady state.
func runBenchmark(ctx context.Context, client *http.Client, template *http.Request, concurrency int, duration, warmup time.Duration, progress func(string)) *benchmarkResult {
	result := &benchmarkResult{
		StatusCounts: make(map[string]int64),
		Errors:       make(map[string]int64),
	}
	var mu sync.Mutex
	latencies := []float64{}
	measureStart := time.Now().Add(warmup)
	deadline := measureStart.Add(duration)
	nextProgress := measureStart.Add(2 * time.Second)
	drain := func(resp *http.Response) {
		// Drain the body (bounded) so the connection returns to the
		// keep-alive pool — an undrained body forces a fresh TCP+TLS
		// handshake for the next request and skews the numbers.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, benchmarkMaxBodyDrain))
		resp.Body.Close()
	}

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				if time.Now().After(deadline) {
					return
				}

				reqCtx, cancel := context.WithTimeout(ctx, benchmarkRequestTimeout)
				req := template.Clone(reqCtx)
				reqStart := time.Now()
				resp, err := client.Do(req)
				elapsed := float64(time.Since(reqStart).Microseconds()) / 1000.0
				if err != nil {
					cancel()
					if time.Now().Before(measureStart) {
						continue // warmup: unscored
					}
					mu.Lock()
					result.FailedRequests++
					result.TotalRequests++
					msg := err.Error()
					if len(msg) > 120 {
						msg = msg[:120]
					}
					result.Errors[msg]++
					latencies = append(latencies, elapsed)
					mu.Unlock()
					continue
				}
				drain(resp)
				cancel()

				if time.Now().Before(measureStart) {
					continue // warmup: unscored
				}
				mu.Lock()
				result.TotalRequests++
				if resp.StatusCode < 400 {
					result.OKRequests++
				}
				key := resp.Status
				if key == "" {
					key = fmt.Sprintf("%d", resp.StatusCode)
				}
				result.StatusCounts[key]++
				latencies = append(latencies, elapsed)
				mu.Unlock()
			}
		}()
	}

	if warmup > 0 {
		progress("Warming up connections and caches…")
		select {
		case <-ctx.Done():
		case <-time.After(warmup):
		}
	}

	// Emit progress lines while the workers run. Uses a local copy so the
	// workers' view of `deadline` is never written concurrently.
	progressDeadline := deadline
	for time.Now().Before(progressDeadline) {
		select {
		case <-ctx.Done():
			// Task canceled: workers stop on their own ctx check; stop the
			// progress ticker too so we do not spin on a closed channel.
			progressDeadline = time.Now()
		case <-time.After(time.Until(nextProgress)):
		}
		nextProgress = nextProgress.Add(2 * time.Second)
		if time.Now().After(deadline) {
			break
		}
		mu.Lock()
		line := fmt.Sprintf("… %d requests, ~%.0f req/s", result.TotalRequests, float64(result.TotalRequests)/time.Since(measureStart).Seconds())
		mu.Unlock()
		progress(line)
	}
	wg.Wait()

	elapsed := time.Since(measureStart).Seconds()
	mu.Lock()
	defer mu.Unlock()
	result.DurationSeconds = elapsed
	if elapsed > 0 {
		result.RequestsPerSecond = float64(result.TotalRequests) / elapsed
	}
	if len(latencies) > 0 {
		sort.Float64s(latencies)
		pick := func(p float64) float64 {
			idx := int(p * float64(len(latencies)-1))
			return latencies[idx]
		}
		var sum float64
		for _, v := range latencies {
			sum += v
		}
		result.LatencyMS = benchmarkLatency{
			Avg: sum / float64(len(latencies)),
			Min: latencies[0],
			P50: pick(0.50),
			P90: pick(0.90),
			P95: pick(0.95),
			P99: pick(0.99),
			Max: latencies[len(latencies)-1],
		}
	}
	if len(result.Errors) > benchmarkMaxErrorSamples {
		trimmed := make(map[string]int64, benchmarkMaxErrorSamples)
		for k, v := range result.Errors {
			if len(trimmed) == benchmarkMaxErrorSamples {
				break
			}
			trimmed[k] = v
		}
		result.Errors = trimmed
	}
	return result
}

func benchmarkSummary(res *benchmarkResult) string {
	return fmt.Sprintf(
		"%d requests in %.1fs — %.0f req/s (ok: %d, failed: %d) | latency ms avg %.1f / p50 %.1f / p95 %.1f / p99 %.1f",
		res.TotalRequests, res.DurationSeconds, res.RequestsPerSecond, res.OKRequests, res.FailedRequests,
		res.LatencyMS.Avg, res.LatencyMS.P50, res.LatencyMS.P95, res.LatencyMS.P99,
	)
}

// BenchmarkWebsite starts a background load test against the site's local
// Nginx chain and returns the task ID. Results are written to the task
// output as progress lines plus a final "##RESULT_JSON## {…}" marker that
// the frontend parses into the results table.
func (s *Service) BenchmarkWebsite(ctx context.Context, id string, opts BenchmarkOptions) (string, error) {
	w, err := s.Get(ctx, id)
	if err != nil {
		return "", err
	}
	opts = normalizeBenchmarkOptions(opts)
	client, template, err := benchmarkClient(w)
	if err != nil {
		return "", fmt.Errorf("prepare benchmark: %w", err)
	}
	tr := s.taskRunner()
	if tr == nil {
		return "", fmt.Errorf("task runner not available")
	}

	return tr.RunFuncWithOptions(
		taskrunner.Options{
			Name:    "Benchmark — " + w.Domain,
			Module:  "website",
			Timeout: time.Duration(opts.DurationSeconds+30) * time.Second,
		},
		func(taskCtx context.Context, write func(string)) error {
			scheme := "http"
			if w.SSLEnabled {
				scheme = "https"
			}
			write(fmt.Sprintf("Target: %s://%s%s (local Nginx, Host: %s)", scheme, w.Domain, opts.Path, w.Domain))
			write(fmt.Sprintf("Load: %d concurrent clients for %ds", opts.Concurrency, opts.DurationSeconds))

			res := runBenchmark(taskCtx, client, template, opts.Concurrency, time.Duration(opts.DurationSeconds)*time.Second, benchmarkWarmup, write)

			write(benchmarkSummary(res))
			if payload, err := json.Marshal(res); err == nil {
				write("##RESULT_JSON## " + string(payload))
			}
			return nil
		},
	), nil
}
