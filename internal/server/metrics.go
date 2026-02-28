package server

import (
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

type metricsCollector struct {
	totalRequests  atomic.Uint64
	totalLatencyMs atomic.Uint64

	mu          sync.Mutex
	statusCount map[int]uint64
}

func newMetricsCollector() *metricsCollector {
	return &metricsCollector{
		statusCount: make(map[int]uint64),
	}
}

func (m *metricsCollector) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		latency := time.Since(start).Milliseconds()

		m.totalRequests.Add(1)
		m.totalLatencyMs.Add(uint64(latency))

		m.mu.Lock()
		m.statusCount[status]++
		m.mu.Unlock()
	}
}

func (m *metricsCollector) handler(c *gin.Context) {
	total := m.totalRequests.Load()
	totalLatency := m.totalLatencyMs.Load()

	var avg float64
	if total > 0 {
		avg = float64(totalLatency) / float64(total)
	}

	c.Header("Content-Type", "text/plain; version=0.0.4")
	_, _ = fmt.Fprintf(c.Writer, "stratum_requests_total %d\n", total)
	_, _ = fmt.Fprintf(c.Writer, "stratum_request_latency_ms_avg %.2f\n", avg)

	m.mu.Lock()
	defer m.mu.Unlock()
	for status, count := range m.statusCount {
		_, _ = fmt.Fprintf(c.Writer,
			"stratum_requests_by_status_total{status=\"%s\"} %d\n",
			strconv.Itoa(status),
			count,
		)
	}
}
