// twMQTTAgent - A lightweight, cross-platform system monitoring agent.
// It collects system metrics (CPU, Memory, Load, Network, Processes) and publishes them
// to an MQTT broker in JSON format, designed for the TWSNMP series.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"runtime"
	"strings"
	"sync"
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

type InterfaceInfo struct {
	Index       int      `json:"index"`
	Name        string   `json:"name"`
	MTU         int      `json:"mtu"`
	MAC         string   `json:"mac"`
	Status      string   `json:"status"` // "up" or "down"
	Addrs       []string `json:"addrs"`
	BytesRecv   uint64   `json:"bytes_recv"`
	BytesSent   uint64   `json:"bytes_sent"`
	PacketsRecv uint64   `json:"packets_recv"`
	PacketsSent uint64   `json:"packets_sent"`
	ErrIn       uint64   `json:"err_in"`
	ErrOut      uint64   `json:"err_out"`
	DropIn      uint64   `json:"drop_in"`
	DropOut     uint64   `json:"drop_out"`
}

type mqttIFData struct {
	Time       string          `json:"time"`
	Host       string          `json:"host"`
	Interfaces []InterfaceInfo `json:"interfaces"`
}

type ARPEntry struct {
	IP        string `json:"ip"`
	MAC       string `json:"mac"`
	Interface string `json:"interface"`
	Type      string `json:"type"` // "dynamic", "static", etc.
}

type mqttARPData struct {
	Time string     `json:"time"`
	Host string     `json:"host"`
	ARP  []ARPEntry `json:"arp"`
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

// collectInterfaceStats gathers network interface configurations and counters.
func collectInterfaceStats() ([]InterfaceInfo, error) {
	ifaces, err := gopsnet.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("gopsnet.Interfaces: %w", err)
	}
	ioStats, err := gopsnet.IOCounters(true)
	if err != nil {
		return nil, fmt.Errorf("gopsnet.IOCounters: %w", err)
	}
	ioMap := make(map[string]gopsnet.IOCountersStat)
	for _, stat := range ioStats {
		ioMap[stat.Name] = stat
	}

	var list []InterfaceInfo
	for _, iface := range ifaces {
		info := InterfaceInfo{
			Index: iface.Index,
			Name:  iface.Name,
			MTU:   iface.MTU,
			MAC:   iface.HardwareAddr,
		}
		status := "down"
		for _, f := range iface.Flags {
			if f == "up" {
				status = "up"
				break
			}
		}
		info.Status = status
		for _, addr := range iface.Addrs {
			info.Addrs = append(info.Addrs, addr.Addr)
		}
		if stat, ok := ioMap[iface.Name]; ok {
			info.BytesRecv = stat.BytesRecv
			info.BytesSent = stat.BytesSent
			info.PacketsRecv = stat.PacketsRecv
			info.PacketsSent = stat.PacketsSent
			info.ErrIn = stat.Errin
			info.ErrOut = stat.Errout
			info.DropIn = stat.Dropin
			info.DropOut = stat.Dropout
		}
		list = append(list, info)
	}
	return list, nil
}

var (
	ipRegex  = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`)
	macRegex = regexp.MustCompile(`\b([0-9a-fA-F]{1,2}[:-]){5}([0-9a-fA-F]{1,2})\b`)
)

// getARPTable retrieves the ARP table entries.
func getARPTable() ([]ARPEntry, error) {
	if runtime.GOOS == "linux" {
		entries, err := parseProcNetArp()
		if err == nil {
			return entries, nil
		}
	}
	return executeArpCommand()
}

func parseProcNetArp() ([]ARPEntry, error) {
	file, err := os.Open("/proc/net/arp")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var entries []ARPEntry
	scanner := bufio.NewScanner(file)
	if scanner.Scan() {
		_ = scanner.Text() // Skip header
	}
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 6 {
			if fields[2] == "0x0" {
				continue
			}
			entries = append(entries, ARPEntry{
				IP:        fields[0],
				MAC:       fields[3],
				Interface: fields[5],
			})
		}
	}
	return entries, scanner.Err()
}

func executeArpCommand() ([]ARPEntry, error) {
	cmd := exec.Command("arp", "-a")
	if runtime.GOOS != "windows" {
		cmd = exec.Command("arp", "-an")
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	var entries []ARPEntry
	scanner := bufio.NewScanner(&out)
	for scanner.Scan() {
		line := scanner.Text()
		ip := ipRegex.FindString(line)
		mac := macRegex.FindString(line)
		if ip != "" && mac != "" {
			mac = strings.ToLower(strings.ReplaceAll(mac, "-", ":"))
			iface := ""
			if runtime.GOOS == "darwin" || runtime.GOOS == "freebsd" {
				if idx := strings.Index(line, " on "); idx != -1 {
					parts := strings.Fields(line[idx+4:])
					if len(parts) > 0 {
						iface = parts[0]
					}
				}
			}
			entryType := ""
			if strings.Contains(line, "dynamic") {
				entryType = "dynamic"
			} else if strings.Contains(line, "static") {
				entryType = "static"
			}

			entries = append(entries, ARPEntry{
				IP:        ip,
				MAC:       mac,
				Interface: iface,
				Type:      entryType,
			})
		}
	}
	return entries, nil
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

// separated publish functions

func publishMonitor(client mqtt.Client, topic string, hostname string) {
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

func publishIF(client mqtt.Client, baseTopic string, hostname string) {
	interfaces, err := collectInterfaceStats()
	if err != nil {
		log.Printf("[ERROR] Failed to collect interface stats: %v", err)
		return
	}

	payload := mqttIFData{
		Time:       time.Now().Format(time.RFC3339),
		Host:       hostname,
		Interfaces: interfaces,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[ERROR] Failed to marshal IF payload: %v", err)
		return
	}

	topic := baseTopic + "/IF/" + hostname
	token := client.Publish(topic, 0, false, data)
	token.Wait()
	if err := token.Error(); err != nil {
		log.Printf("[ERROR] Failed to publish IF message: %v", err)
		return
	}
	log.Printf("[INFO] Published IF: %d interfaces", len(interfaces))
}

func publishArp(client mqtt.Client, baseTopic string, hostname string) {
	arpEntries, err := getARPTable()
	if err != nil {
		log.Printf("[ERROR] Failed to get ARP table: %v", err)
		return
	}

	payload := mqttARPData{
		Time: time.Now().Format(time.RFC3339),
		Host: hostname,
		ARP:  arpEntries,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[ERROR] Failed to marshal Arp payload: %v", err)
		return
	}

	topic := baseTopic + "/Arp/" + hostname
	token := client.Publish(topic, 0, false, data)
	token.Wait()
	if err := token.Error(); err != nil {
		log.Printf("[ERROR] Failed to publish Arp message: %v", err)
		return
	}
	log.Printf("[INFO] Published Arp: %d entries", len(arpEntries))
}

func main() {
	// --- CLI Flags ---
	broker := flag.String("broker", "tcp://localhost:1883", "MQTT broker URL (e.g., tcp://host:1883)")
	clientID := flag.String("client-id", "twMQTTAgent", "MQTT client ID")
	baseTopic := flag.String("topic", "twMQTTAgent", "Base MQTT topic; hostname is appended automatically (e.g., twMQTTAgent/Monitor/<hostname>)")
	interval := flag.Int("interval", 30, "Publish interval for system metrics (Monitor) in seconds")
	ifInterval := flag.Int("if-interval", 0, "Publish interval for interface stats (ifTable/IF) in seconds (0 to disable)")
	arpInterval := flag.Int("arp-interval", 0, "Publish interval for ARP table (Arp) in seconds (0 to disable)")
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

	topic := *baseTopic + "/Monitor/" + hostname

	log.Printf("[INFO] twMQTTAgent starting — hostname=%s broker=%s baseTopic=%s interval=%ds ifInterval=%ds arpInterval=%ds",
		hostname, *broker, *baseTopic, *interval, *ifInterval, *arpInterval)

	client, err := connectMQTT(*broker, *clientID, *user, *password)
	if err != nil {
		log.Fatalf("[FATAL] Failed to connect to MQTT broker: %v", err)
	}
	defer client.Disconnect(250)

	// --- Graceful shutdown on SIGINT/SIGTERM ---
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("[INFO] Received signal %v, shutting down.", sig)
		cancel()
	}()

	var wg sync.WaitGroup

	// Monitor Loop
	if *interval > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(time.Duration(*interval) * time.Second)
			defer ticker.Stop()

			// Publish immediately
			publishMonitor(client, topic, hostname)

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					publishMonitor(client, topic, hostname)
				}
			}
		}()
	}

	// IF Loop
	if *ifInterval > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(time.Duration(*ifInterval) * time.Second)
			defer ticker.Stop()

			// Publish immediately
			publishIF(client, *baseTopic, hostname)

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					publishIF(client, *baseTopic, hostname)
				}
			}
		}()
	}

	// ARP Loop
	if *arpInterval > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(time.Duration(*arpInterval) * time.Second)
			defer ticker.Stop()

			// Publish immediately
			publishArp(client, *baseTopic, hostname)

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					publishArp(client, *baseTopic, hostname)
				}
			}
		}()
	}

	// Wait for shutdown signal
	<-ctx.Done()
	wg.Wait()
	log.Println("[INFO] twMQTTAgent stopped cleanly.")
}
