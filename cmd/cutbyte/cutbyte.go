package main

import (
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/guonaihong/cutbyte"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	// 配置日志 - 使用slog替代logrus
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(logger)

	// 创建代理实例
	proxy := cutbyte.NewProxy()

	// 初始化默认配置
	defaultConfig := &cutbyte.ProxyConfig{
		Routes: []*cutbyte.RouteConfig{
			{
				Prefix: "/api",
				PrimaryGroups: []cutbyte.GroupConfig{
					{
						Name:     "service-group",
						Backends: []string{"127.0.0.1:8081", "127.0.0.1:8082"},
						Strategy: "round_robin",
						Weights:  []int{100, 50},
						HealthCheck: cutbyte.HealthCheckConfig{
							Path:     "/health",
							Interval: 10 * time.Second,
							Timeout:  2 * time.Second,
						},
					},
				},
				Weights: []int{100},
				ReplicateGroups: []cutbyte.GroupConfig{
					{
						Name:     "replica-group",
						Backends: []string{"127.0.0.1:8083"},
						Strategy: "round_robin",
						HealthCheck: cutbyte.HealthCheckConfig{
							Path:     "/health",
							Interval: 10 * time.Second,
							Timeout:  2 * time.Second,
						},
					},
				},
			},
		},
	}

	// 应用默认配置
	if err := proxy.UpdateConfig(defaultConfig); err != nil {
		log.Fatalf("Failed to apply default config: %v", err)
	}

	// 启动监控服务器
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		http.ListenAndServe(":9100", nil)
	}()

	// 启动配置更新服务器
	go func() {
		http.HandleFunc("/update-config", proxy.UpdateConfigHandler)
		log.Println("Starting config server on :8081")
		log.Fatal(http.ListenAndServe(":8081", nil))
	}()

	// 启动网关
	httpServer := &http.Server{
		Addr:    ":8080",
		Handler: cutbyte.LoggingMiddleware(proxy),
	}

	log.Println("Starting gateway on :8080")
	log.Fatal(httpServer.ListenAndServe())
}
