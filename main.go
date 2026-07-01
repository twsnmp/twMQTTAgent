// twMQTTAgent - A lightweight, cross-platform system monitoring agent.
// It collects system metrics (CPU, Memory, Load, Network, Processes) and publishes them
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
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	gopsnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

var lastMonitorTime int64
var lastBytesRecv uint64
var lastBytesSent uint64

type mqttMonitorDataEnt struct {
	Time    string  `json:"time"`
	Host    string  `json:"host"`
	CPU     float64 `json:"cpu"`
	Memory  float64 `json:"memory"`
	Load    float64 `json:"load"`
	Sent    uint64  `json:"sent"`
	Recv    uint64  `json:"recv"`
	TxSpeed float64 `json:"tx_speed"`
	RxSpeed float64 `json:"rx_speed"`
	Process int     `json:"process"`
}

// collectMetrics gathers current system metrics.
func collectMetrics(hostname string) (mqttMonitorDataEnt, error) {
	var m mqttMonitorDataEnt
	m.Time = time.Now().Format(time.RFC3339)
	m.Host = hostname

	// CPU
	cpus, err := cpu.Percent(0, false)
	if err != nil {
		return m, fmt.Errorf("cpu.Percent: %w", err)
	}
	if len(cpus) > 0 {
		m.CPU = cpus[0]
	}

	// Load
	loads, err := load.Avg()
	if err != nil {
		return m, fmt.Errorf("load.Avg: %w", err)
	}
	m.Load = loads.Load1

	// Memory
	vmStat, err := mem.VirtualMemory()
	if err != nil {
		return m, fmt.Errorf("mem.VirtualMemory: %w", err)
	}
	m.Memory = vmStat.UsedPercent

	// Net
	nets, err := gopsnet.IOCounters(false)
	if err != nil {
		return m, fmt.Errorf("net.IOCounters: %w", err)
	}
	if len(nets) > 0 {
		now := time.Now().Unix()
		if lastMonitorTime > 0 {
			diff := now - lastMonitorTime
			if diff > 0 {
				dSent := nets[0].BytesSent - lastBytesSent
				dRecv := nets[0].BytesRecv - lastBytesRecv
				rxSpeed := 8.0 * float64(dRecv) / float64(diff)
				rxSpeed /= (1000 * 1000)
				txSpeed := 8.0 * float64(dSent) / float64(diff)
				txSpeed /= (1000 * 1000)

				m.Recv = dRecv
				m.Sent = dSent
				m.TxSpeed = txSpeed
				m.RxSpeed = rxSpeed
			}
		}
		lastMonitorTime = now
		lastBytesRecv = nets[0].BytesRecv
		lastBytesSent = nets[0].BytesSent
	}

	// Processes
	pids, err := process.Pids()
	if err != nil {
		return m, fmt.Errorf("process.Pids: %w", err)
	}
	m.Process = len(pids)

	return m, nil
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
	broker := flag.String("broker", "tcp://localhost:1883", "MQTT broker URL (e.g., tcp://host:1883)")
	clientID := flag.String("client-id", "twMQTTAgent", "MQTT client ID")
	baseTopic := flag.String("topic", "twMQTTAgent", "Base MQTT topic; hostname is appended automatically (e.g., twMQTTAgent/Monitor/<hostname>)")
	interval := flag.Int("interval", 30, "Publish interval in seconds")
	user := flag.String("user", "", "MQTT username (optional)")
	password := flag.String("password", "", "MQTT password (optional)")
	hostFlag := flag.String("hostname", "", "Hostname used in topic and payload; defaults to system hostname if empty")
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
	topic := *baseTopic + "/Monitor/" + hostname

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
		mqttData, err := collectMetrics(hostname)
		if err != nil {
			log.Printf("[ERROR] Failed to collect metrics: %v", err)
			return
		}

		data, err := json.Marshal(mqttData)
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
		log.Printf("[INFO] Published: cpu=%.1f%% mem=%.1f%% load=%.3f process=%d txSpeed=%.3f rxSpeed=%.3f",
			mqttData.CPU, mqttData.Memory, mqttData.Load, mqttData.Process, mqttData.TxSpeed, mqttData.RxSpeed)
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
