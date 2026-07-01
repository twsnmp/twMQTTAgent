// twMQTTAgent - A lightweight, cross-platform system monitoring agent.
// It collects system metrics (CPU, Memory, Disk) and publishes them
// to an MQTT broker in JSON format, designed for the TWSNMP series.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
)

// MemoryMetrics holds memory usage statistics.
type MemoryMetrics struct {
	TotalGB float64 `json:"total_gb"`
	UsedGB  float64 `json:"used_gb"`
	FreeGB  float64 `json:"free_gb"`
	Percent float64 `json:"percent"`
}

// DiskMetrics holds disk usage statistics.
type DiskMetrics struct {
	TotalGB float64 `json:"total_gb"`
	UsedGB  float64 `json:"used_gb"`
	FreeGB  float64 `json:"free_gb"`
	Percent float64 `json:"percent"`
}

// Metrics holds all collected system metrics.
type Metrics struct {
	CPUPercent float64       `json:"cpu_percent"`
	Memory     MemoryMetrics `json:"memory"`
	Disk       DiskMetrics   `json:"disk"`
}

// Payload is the top-level JSON structure published to the MQTT broker.
type Payload struct {
	Hostname  string  `json:"hostname"`
	Timestamp int64   `json:"timestamp"`
	Status    string  `json:"status"`
	Metrics   Metrics `json:"metrics"`
}

const gbDivisor = 1024 * 1024 * 1024

// collectMetrics gathers current system metrics.
func collectMetrics() (Metrics, error) {
	var m Metrics

	// CPU: measure over 1 second interval for accuracy
	cpuPercents, err := cpu.Percent(time.Second, false)
	if err != nil {
		return m, fmt.Errorf("cpu.Percent: %w", err)
	}
	if len(cpuPercents) > 0 {
		m.CPUPercent = roundFloat(cpuPercents[0], 1)
	}

	// Memory
	vmStat, err := mem.VirtualMemory()
	if err != nil {
		return m, fmt.Errorf("mem.VirtualMemory: %w", err)
	}
	m.Memory = MemoryMetrics{
		TotalGB: roundFloat(float64(vmStat.Total)/gbDivisor, 1),
		UsedGB:  roundFloat(float64(vmStat.Used)/gbDivisor, 1),
		FreeGB:  roundFloat(float64(vmStat.Free)/gbDivisor, 1),
		Percent: roundFloat(vmStat.UsedPercent, 1),
	}

	// Disk (root partition)
	diskStat, err := disk.Usage("/")
	if err != nil {
		return m, fmt.Errorf("disk.Usage: %w", err)
	}
	m.Disk = DiskMetrics{
		TotalGB: roundFloat(float64(diskStat.Total)/gbDivisor, 1),
		UsedGB:  roundFloat(float64(diskStat.Used)/gbDivisor, 1),
		FreeGB:  roundFloat(float64(diskStat.Free)/gbDivisor, 1),
		Percent: roundFloat(diskStat.UsedPercent, 1),
	}

	return m, nil
}

// roundFloat rounds a float64 to d decimal places.
func roundFloat(val float64, d int) float64 {
	factor := 1.0
	for i := 0; i < d; i++ {
		factor *= 10
	}
	return float64(int(val*factor+0.5)) / factor
}

// buildPayload constructs the JSON payload.
func buildPayload(hostname string, metrics Metrics) Payload {
	return Payload{
		Hostname:  hostname,
		Timestamp: time.Now().Unix(),
		Status:    "online",
		Metrics:   metrics,
	}
}

// connectMQTT establishes an MQTT connection and returns the client.
func connectMQTT(broker, clientID, user, password string) (mqtt.Client, error) {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(broker)
	opts.SetClientID(clientID)
	opts.SetAutoReconnect(true)
	opts.SetCleanSession(true)

	if user != "" {
		opts.SetUsername(user)
	}
	if password != "" {
		opts.SetPassword(password)
	}

	opts.OnConnect = func(_ mqtt.Client) {
		log.Println("[INFO] Connected to MQTT broker:", broker)
	}
	opts.OnConnectionLost = func(_ mqtt.Client, err error) {
		log.Printf("[WARN] Connection lost: %v. Reconnecting...", err)
	}

	client := mqtt.NewClient(opts)
	token := client.Connect()
	token.Wait()
	if err := token.Error(); err != nil {
		return nil, fmt.Errorf("mqtt.Connect: %w", err)
	}
	return client, nil
}

func main() {
	// --- CLI Flags ---
	broker    := flag.String("broker",    "tcp://localhost:1883", "MQTT broker URL (e.g., tcp://host:1883)")
	clientID  := flag.String("client-id", "twMQTTAgent",         "MQTT client ID")
	baseTopic := flag.String("topic",     "twsnmp/agent",        "Base MQTT topic; hostname is appended automatically (e.g., twsnmp/agent/<hostname>)")
	interval  := flag.Int("interval",     30,                    "Publish interval in seconds")
	user      := flag.String("user",      "",                    "MQTT username (optional)")
	password  := flag.String("password",  "",                    "MQTT password (optional)")
	hostFlag  := flag.String("hostname",  "",                    "Hostname used in topic and payload; defaults to system hostname if empty")
	flag.Parse()

	// Resolve effective hostname: CLI flag takes precedence over system hostname.
	hostname, err := os.Hostname()
	if err != nil {
		log.Fatalf("[FATAL] Failed to get system hostname: %v", err)
	}
	if *hostFlag != "" {
		hostname = *hostFlag
	}

	// Build the full topic by appending the hostname.
	topic := *baseTopic + "/" + hostname

	log.Printf("[INFO] twMQTTAgent starting — hostname=%s broker=%s topic=%s interval=%ds",
		hostname, *broker, topic, *interval)

	client, err := connectMQTT(*broker, *clientID, *user, *password)
	if err != nil {
		log.Fatalf("[FATAL] Failed to connect to MQTT broker: %v", err)
	}
	defer client.Disconnect(250)

	// --- Graceful shutdown on SIGINT/SIGTERM ---
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(time.Duration(*interval) * time.Second)
	defer ticker.Stop()

	// Publish immediately on startup, then on each tick.
	publish := func() {
		metrics, err := collectMetrics()
		if err != nil {
			log.Printf("[ERROR] Failed to collect metrics: %v", err)
			return
		}

		payload := buildPayload(hostname, metrics)
		data, err := json.Marshal(payload)
		if err != nil {
			log.Printf("[ERROR] Failed to marshal payload: %v", err)
			return
		}

		token := client.Publish(topic, 0, false, data)
		token.Wait()
		if err := token.Error(); err != nil {
			log.Printf("[ERROR] Failed to publish message: %v", err)
			return
		}
		log.Printf("[INFO] Published: cpu=%.1f%% mem=%.1f%% disk=%.1f%%",
			metrics.CPUPercent, metrics.Memory.Percent, metrics.Disk.Percent)
	}

	publish()

	for {
		select {
		case <-ticker.C:
			publish()
		case sig := <-sigCh:
			log.Printf("[INFO] Received signal %v, shutting down.", sig)
			return
		}
	}
}
