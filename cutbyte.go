package cutbyte

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"expvar"
	"fmt"
	"io"
	"log"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// 初始化日志和监控
var (
	logger = slog.Default()
	reqCnt = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gateway_requests_total",
			Help: "Number of HTTP requests processed by the gateway",
		},
		[]string{"code", "method", "path"},
	)
	reqLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "gateway_request_latency_seconds",
			Help:    "Latency of HTTP requests processed by the gateway",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"path"},
	)
	activeBackends = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gateway_backend_active",
			Help: "Whether a backend is active (1) or inactive (0)",
		},
		[]string{"backend"},
	)
)

func init() {
	prometheus.MustRegister(reqCnt, reqLatency, activeBackends)
	expvar.Publish("version", expvar.NewString("1.0.0"))
}

// Backend represents an upstream server
type Backend struct {
	Address         string
	Timeout         time.Duration
	Active          int32 // atomic
	HealthCheckPath string
	connections     int32
}

func (b *Backend) IsActive() bool {
	return atomic.LoadInt32(&b.Active) == 1
}

func (b *Backend) setInactive() {
	atomic.StoreInt32(&b.Active, 0)
}

func (b *Backend) setActive() {
	atomic.StoreInt32(&b.Active, 1)
}

// LoadBalancer interface for selecting backends
type LoadBalancer interface {
	Next() (*Backend, error)
	UpdateBackends([]*Backend)
}

// RoundRobinLB implements round-robin load balancing
type RoundRobinLB struct {
	mu       sync.Mutex
	backends []*Backend
	current  int
}

func NewRoundRobin(backends []*Backend) LoadBalancer {
	return &RoundRobinLB{
		backends: backends,
		current:  0,
	}
}

func (r *RoundRobinLB) Next() (*Backend, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.backends) == 0 {
		return nil, errors.New("no backends")
	}
	for i := 0; i < len(r.backends); i++ {
		be := r.backends[r.current]
		r.current = (r.current + 1) % len(r.backends)
		if be.IsActive() {
			return be, nil
		}
	}
	return nil, errors.New("all backends inactive")
}

func (r *RoundRobinLB) UpdateBackends(backends []*Backend) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.backends = backends
}

// WeightedRoundRobinLB implements weighted round-robin load balancing
type WeightedRoundRobinLB struct {
	mu       sync.Mutex
	backends []*Backend
	total    int
	current  int
	virtual  []int
	weights  []int
	indexMap map[int]int // virtual index to backend index
}

func NewWeightedRoundRobin(backends []*Backend, weights []int) LoadBalancer {
	if len(backends) != len(weights) {
		panic("backends and weights must have the same length")
	}
	virtual := make([]int, 0)
	indexMap := make(map[int]int)
	total := 0
	for i, w := range weights {
		if w <= 0 {
			continue
		}
		for j := 0; j < w*10; j++ {
			virtual = append(virtual, total+j)
			indexMap[total+j] = i
		}
		total += w * 10
	}
	return &WeightedRoundRobinLB{
		backends: backends,
		total:    total,
		virtual:  virtual,
		indexMap: indexMap,
		weights:  weights,
	}
}

func (w *WeightedRoundRobinLB) Next() (*Backend, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.backends) == 0 {
		return nil, errors.New("no backends")
	}
	if w.total == 0 {
		return nil, errors.New("all backends inactive")
	}
	randVal := rand.Intn(w.total)
	for i := 0; i < len(w.virtual); i++ {
		v := w.virtual[(w.current+i)%w.total]
		if v >= randVal {
			w.current = (w.current + i + 1) % w.total
			idx := w.indexMap[v]
			if w.backends[idx].IsActive() {
				return w.backends[idx], nil
			}
		}
	}
	return nil, errors.New("all backends inactive")
}

func (w *WeightedRoundRobinLB) UpdateBackends(backends []*Backend) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.backends = backends
}

// LeastConnectionsLB implements least connections load balancing
type LeastConnectionsLB struct {
	mu       sync.Mutex
	backends []*Backend
}

func NewLeastConnections(backends []*Backend) LoadBalancer {
	return &LeastConnectionsLB{
		backends: backends,
	}
}

func (l *LeastConnectionsLB) Next() (*Backend, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.backends) == 0 {
		return nil, errors.New("no backends")
	}
	var selected *Backend
	for _, be := range l.backends {
		if !be.IsActive() {
			continue
		}
		if selected == nil || atomic.LoadInt32(&be.connections) < atomic.LoadInt32(&selected.connections) {
			selected = be
		}
	}
	if selected == nil {
		return nil, errors.New("all backends inactive")
	}
	atomic.AddInt32(&selected.connections, 1)
	return selected, nil
}

func (l *LeastConnectionsLB) UpdateBackends(backends []*Backend) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.backends = backends
}

// BackendGroup contains a group of backends
type BackendGroup struct {
	Name          string
	Backends      []*Backend
	LB            LoadBalancer
	HealthCheck   *HealthCheckConfig
	healthChecker *HealthChecker
}

type HealthCheckConfig struct {
	Path     string
	Interval time.Duration
	Timeout  time.Duration
}

func NewBackendGroup(name string, backends []*Backend, lb LoadBalancer, hc *HealthCheckConfig) *BackendGroup {
	bg := &BackendGroup{
		Name:     name,
		Backends: backends,
		LB:       lb,
	}
	if hc != nil {
		bg.HealthCheck = hc
		bg.healthChecker = NewHealthChecker(bg, hc)
		bg.healthChecker.Start()
	}
	return bg
}

// HealthChecker manages health checks for a backend group
type HealthChecker struct {
	group    *BackendGroup
	config   *HealthCheckConfig
	stopChan chan struct{}
}

func NewHealthChecker(group *BackendGroup, config *HealthCheckConfig) *HealthChecker {
	return &HealthChecker{
		group:    group,
		config:   config,
		stopChan: make(chan struct{}),
	}
}

func (hc *HealthChecker) Start() {
	ticker := time.NewTicker(hc.config.Interval)
	go func() {
		for {
			select {
			case <-ticker.C:
				hc.checkHealth()
			case <-hc.stopChan:
				ticker.Stop()
				return
			}
		}
	}()
}

func (hc *HealthChecker) Stop() {
	close(hc.stopChan)
}

func (hc *HealthChecker) checkHealth() {
	for _, backend := range hc.group.Backends {
		// 创建健康检查客户端
		client := &http.Client{
			Timeout: hc.config.Timeout,
		}
		url := fmt.Sprintf("http://%s%s", backend.Address, backend.HealthCheckPath)
		resp, err := client.Get(url)
		if err != nil || resp.StatusCode != http.StatusOK {
			if backend.IsActive() {
				logger.Warn("Backend is down", "address", backend.Address)
				backend.setInactive()
				activeBackends.WithLabelValues(backend.Address).Set(0)
			}
		} else {
			if !backend.IsActive() {
				logger.Info("Backend is up", "address", backend.Address)
				backend.setActive()
				activeBackends.WithLabelValues(backend.Address).Set(1)
			}
		}
	}
}

// Route defines a routing rule
type Route struct {
	Prefix          string
	PrimaryGroups   []*BackendGroup
	Weights         []int
	ReplicateGroups []*BackendGroup
}

// ProxyConfig is the dynamic configuration for the proxy
type ProxyConfig struct {
	Routes []*RouteConfig `json:"routes"`
}

type RouteConfig struct {
	Prefix          string        `json:"prefix"`
	PrimaryGroups   []GroupConfig `json:"primary_groups"`
	Weights         []int         `json:"weights"`
	ReplicateGroups []GroupConfig `json:"replicate_groups"`
}

type GroupConfig struct {
	Name        string            `json:"name"`
	Backends    []string          `json:"backends"`
	Strategy    string            `json:"strategy"`
	Weights     []int             `json:"weights,omitempty"`
	HealthCheck HealthCheckConfig `json:"health_check,omitempty"`
}

// Proxy is the main gateway structure
type Proxy struct {
	routes []*Route
	mutex  sync.RWMutex
	config *ProxyConfig
}

func NewProxy() *Proxy {
	return &Proxy{
		routes: make([]*Route, 0),
	}
}

func (p *Proxy) AddRoute(route *Route) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.routes = append(p.routes, route)
}

func (p *Proxy) UpdateConfig(config *ProxyConfig) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// 清除现有路由
	p.routes = make([]*Route, 0)

	// 创建新路由
	for _, routeCfg := range config.Routes {
		primaryGroups := make([]*BackendGroup, 0)
		for _, groupCfg := range routeCfg.PrimaryGroups {
			// 创建后端实例
			backends := make([]*Backend, len(groupCfg.Backends))
			for j, addr := range groupCfg.Backends {
				backends[j] = &Backend{
					Address:         addr,
					Timeout:         5 * time.Second,
					HealthCheckPath: groupCfg.HealthCheck.Path,
				}
			}

			// 创建负载均衡器
			var lb LoadBalancer
			switch groupCfg.Strategy {
			case "round_robin":
				lb = NewRoundRobin(backends)
			case "weighted_round_robin":
				lb = NewWeightedRoundRobin(backends, routeCfg.Weights)
			case "least_connections":
				lb = NewLeastConnections(backends)
			default:
				return fmt.Errorf("unknown strategy: %s", groupCfg.Strategy)
			}

			// 创建后端组
			healthCheck := &groupCfg.HealthCheck
			if healthCheck.Interval == 0 {
				healthCheck = nil
			}

			bg := NewBackendGroup(groupCfg.Name, backends, lb, healthCheck)
			primaryGroups = append(primaryGroups, bg)
		}

		// 创建路由
		route := &Route{
			Prefix:          routeCfg.Prefix,
			PrimaryGroups:   primaryGroups,
			Weights:         routeCfg.Weights,
			ReplicateGroups: make([]*BackendGroup, 0), // 可以类似处理复制组
		}
		p.routes = append(p.routes, route)
	}

	// 更新配置
	p.config = config
	return nil
}

func (p *Proxy) match(path string) *Route {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	var longestMatch string
	var selectedRoute *Route

	for _, route := range p.routes {
		if strings.HasPrefix(path, route.Prefix) {
			if len(route.Prefix) > len(longestMatch) {
				longestMatch = route.Prefix
				selectedRoute = route
			}
		}
	}
	return selectedRoute
}

func (p *Proxy) selectPrimaryGroup(route *Route) (*BackendGroup, error) {
	if len(route.Weights) == 0 || len(route.PrimaryGroups) == 0 {
		return nil, errors.New("no primary groups or weights")
	}

	total := 0
	for _, w := range route.Weights {
		total += w
	}

	if total == 0 {
		return nil, errors.New("zero total weight")
	}

	randVal := rand.Intn(total)
	sum := 0
	for i, w := range route.Weights {
		sum += w
		if randVal < sum {
			return route.PrimaryGroups[i], nil
		}
	}
	return route.PrimaryGroups[len(route.PrimaryGroups)-1], nil
}

func (p *Proxy) handleReplicate(originalReq *http.Request, body []byte, replicateGroups []*BackendGroup) {
	for _, group := range replicateGroups {
		if len(group.Backends) == 0 {
			continue
		}
		backend, err := group.LB.Next()
		if err != nil {
			log.Printf("Failed to select backend for replication: %v", err)
			continue
		}
		newReq := originalReq.Clone(context.Background())
		newReq.Body = io.NopCloser(bytes.NewBuffer(body))
		newReq.ContentLength = int64(len(body))

		go func(req *http.Request, be *Backend) {
			proxy := buildProxy(be)
			proxy.ServeHTTP(httptest.NewRecorder(), req)
		}(newReq, backend)
	}
}

func buildProxy(backend *Backend) *httputil.ReverseProxy {
	target, _ := url.Parse("http://" + backend.Address)
	return &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
		},
		Transport: &http.Transport{
			Proxy: http.ProxyURL(nil),
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ResponseHeaderTimeout: backend.Timeout,
			TLSHandshakeTimeout:   10 * time.Second,
		},
	}
}

func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &responseWriterWrapper{ResponseWriter: w}

		next.ServeHTTP(ww, r)

		latency := time.Since(start).Seconds()
		reqCnt.WithLabelValues(
			fmt.Sprintf("%d", ww.status),
			r.Method,
			r.URL.Path,
		).Inc()
		reqLatency.WithLabelValues(r.URL.Path).Observe(latency)

		logger.Info("Request processed",
			"method", r.Method,
			"path", r.URL.Path,
			"client_ip", r.RemoteAddr,
			"status", ww.status,
			"latency", latency,
			"user_agent", r.UserAgent(),
		)
	})
}

type responseWriterWrapper struct {
	http.ResponseWriter
	status int
}

func (w *responseWriterWrapper) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Read and buffer the request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusInternalServerError)
		return
	}
	r.Body.Close()
	r.Body = io.NopCloser(bytes.NewBuffer(body))

	// Match route
	route := p.match(r.URL.Path)
	if route == nil {
		http.NotFound(w, r)
		return
	}

	// Handle replication
	if len(route.ReplicateGroups) > 0 {
		go p.handleReplicate(r, body, route.ReplicateGroups)
	}

	// Handle primary group selection
	if len(route.PrimaryGroups) == 0 {
		http.Error(w, "No primary groups configured", http.StatusInternalServerError)
		return
	}
	selectedGroup, err := p.selectPrimaryGroup(route)
	if err != nil {
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		return
	}

	// Select backend
	backend, err := selectedGroup.LB.Next()
	if err != nil {
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		return
	}

	// Create and configure proxy
	proxy := buildProxy(backend)
	proxy.ServeHTTP(w, r)
}

// API handler for dynamic config update
func (p *Proxy) UpdateConfigHandler(w http.ResponseWriter, r *http.Request) {
	var config ProxyConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		http.Error(w, "Invalid config format", http.StatusBadRequest)
		return
	}

	if err := p.UpdateConfig(&config); err != nil {
		http.Error(w, fmt.Sprintf("Config update failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Configuration updated successfully")
}
