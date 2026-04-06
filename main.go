// Turtle WoW TCP proxy.
//
// Routes WoW 1.12 traffic through a VPS to work around ISP routing issues.
// Intercepts CMD_REALM_LIST (opcode 0x10) and replaces it with a packet
// built from config.yaml. All other packets (including 2FA/PIN data in
// 0x01) are forwarded byte-for-byte without modification.
//
// Usage: turtle-proxy [config.yaml]
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

type Realm struct {
	Name       string  `yaml:"name"`
	Icon       uint32  `yaml:"icon"`
	Flags      uint8   `yaml:"flags"`
	Population float32 `yaml:"population"`
	Category   uint8   `yaml:"category"`
	RealAddr   string  `yaml:"real_addr"`
	ProxyPort  int     `yaml:"proxy_port"`
}

type Config struct {
	AuthServerHost string  `yaml:"auth_server_host"`
	AuthServerPort int     `yaml:"auth_server_port"`
	ListenHost     string  `yaml:"listen_host"`
	AuthListenPort int     `yaml:"auth_listen_port"`
	ProxyIP        string  `yaml:"proxy_ip"` // optional; defaults to listen_host
	Realms         []Realm `yaml:"realms"`
}

func loadConfig(path string) (*Config, error) {
	cfg := &Config{
		ListenHost:     "0.0.0.0",
		AuthListenPort: 3724,
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return cfg, yaml.NewDecoder(f).Decode(cfg)
}

// ---------------------------------------------------------------------------
// Realm list builder
// ---------------------------------------------------------------------------

// buildRealmList constructs CMD_REALM_LIST (0x10) from config.
//
// Packet layout (Turtle WoW / 1.x auth server):
//
//	uint8   0x10        opcode
//	uint16  size LE     byte count after this field
//	uint32  0           unused
//	uint8   count
//	per realm:
//	  uint32  icon      realm type (6=PVP, 1=Normal)
//	  uint8   flags     (0x40=NEW, 0x20=RECOMMENDED, 0x02=OFFLINE …)
//	  cstring name      null-terminated
//	  cstring addr      null-terminated "IP:PORT"
//	  float32 population LE
//	  uint8   num_chars always 0 (unavailable without server DB)
//	  uint8   category
//	  uint8   0x00
//	uint16  0x0002      footer
func buildRealmList(cfg *Config) []byte {
	var payload bytes.Buffer

	binary.Write(&payload, binary.LittleEndian, uint32(0))
	payload.WriteByte(uint8(len(cfg.Realms)))

	for _, r := range cfg.Realms {
		addr := net.JoinHostPort(cfg.ProxyIP, strconv.Itoa(r.ProxyPort))
		binary.Write(&payload, binary.LittleEndian, r.Icon)
		payload.WriteByte(r.Flags)
		payload.WriteString(r.Name)
		payload.WriteByte(0)
		payload.WriteString(addr)
		payload.WriteByte(0)
		binary.Write(&payload, binary.LittleEndian, r.Population)
		payload.WriteByte(0) // num_chars
		payload.WriteByte(r.Category)
		payload.WriteByte(0) // unk
	}

	binary.Write(&payload, binary.LittleEndian, uint16(0x0002))

	data := payload.Bytes()
	pkt := make([]byte, 3+len(data))
	pkt[0] = 0x10
	binary.LittleEndian.PutUint16(pkt[1:3], uint16(len(data)))
	copy(pkt[3:], data)
	return pkt
}

// ---------------------------------------------------------------------------
// Pipe helpers
// ---------------------------------------------------------------------------

func simplePipe(src, dst net.Conn, counter *int64) {
	buf := make([]byte, 65536)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			*counter += int64(n)
			if _, werr := dst.Write(buf[:n]); werr != nil {
				break
			}
		}
		if err != nil {
			break
		}
	}
}

// pipeServerToClient forwards server→client traffic, replacing any
// CMD_REALM_LIST (0x10) packet with one built from config.
func pipeServerToClient(src, dst net.Conn, cfg *Config, recv *int64) {
	var buf []byte
	tmp := make([]byte, 4096)

	for {
		n, err := src.Read(tmp)
		if n > 0 {
			*recv += int64(n)
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			if len(buf) > 0 {
				dst.Write(buf)
			}
			break
		}

		for len(buf) > 0 {
			if buf[0] != 0x10 {
				dst.Write(buf)
				buf = nil
				break
			}
			if len(buf) < 3 {
				break // wait for size field
			}
			pktSize := binary.LittleEndian.Uint16(buf[1:3])
			total := 3 + int(pktSize)
			if len(buf) < total {
				break // wait for full packet
			}

			patched := buildRealmList(cfg)
			log.Printf("Replaced realm list: server=%d bytes, ours=%d bytes", total, len(patched))
			dst.Write(patched)
			buf = buf[total:]
		}
	}
}

// newDialer returns a dialer that binds outgoing connections to ListenHost
// (when it's a specific IP, not 0.0.0.0).
func newDialer(cfg *Config) *net.Dialer {
	d := &net.Dialer{Timeout: 10 * time.Second}
	if cfg.ListenHost != "0.0.0.0" && cfg.ListenHost != "" {
		d.LocalAddr = &net.TCPAddr{IP: net.ParseIP(cfg.ListenHost)}
	}
	return d
}

// biPipe runs two goroutines piping a↔b, closes both when either ends.
func biPipe(a, b net.Conn, sent, recv *int64, s2cFn func(net.Conn, net.Conn, *int64)) {
	done := make(chan struct{}, 2)
	go func() { simplePipe(a, b, sent); done <- struct{}{} }()
	go func() { s2cFn(b, a, recv); done <- struct{}{} }()
	<-done
	a.Close()
	b.Close()
	<-done
}

// ---------------------------------------------------------------------------
// World proxy
// ---------------------------------------------------------------------------

func handleWorldConn(client net.Conn, realHost string, realPort int, cfg *Config) {
	defer client.Close()
	addr := client.RemoteAddr().(*net.TCPAddr)
	start := time.Now()

	server, err := newDialer(cfg).Dial("tcp", net.JoinHostPort(realHost, strconv.Itoa(realPort)))
	if err != nil {
		log.Printf("World  %s: upstream unreachable %s:%d: %v", addr, realHost, realPort, err)
		return
	}
	defer server.Close()

	log.Printf("World  %s -> %s:%d", addr, realHost, realPort)
	var sent, recv int64
	biPipe(client, server, &sent, &recv, func(src, dst net.Conn, c *int64) { simplePipe(src, dst, c) })

	log.Printf("World  %s closed sent=%d recv=%d dur=%.1fs",
		addr, sent, recv, time.Since(start).Seconds())
}

func startWorldProxy(cfg *Config) error {
	for _, realm := range cfg.Realms {
		host, portStr, err := net.SplitHostPort(realm.RealAddr)
		if err != nil {
			return fmt.Errorf("invalid real_addr %q: %w", realm.RealAddr, err)
		}
		realPort, _ := strconv.Atoi(portStr)
		proxyPort := realm.ProxyPort

		ln, err := net.Listen("tcp", net.JoinHostPort(cfg.ListenHost, strconv.Itoa(proxyPort)))
		if err != nil {
			return fmt.Errorf("world proxy :%d: %w", proxyPort, err)
		}
		log.Printf("World  proxy %s:%d -> %s:%d (%s)", cfg.ListenHost, proxyPort, host, realPort, realm.Name)

		go func(ln net.Listener, h string, p int) {
			for {
				conn, err := ln.Accept()
				if err != nil {
					break
				}
				go handleWorldConn(conn, h, p, cfg)
			}
		}(ln, host, realPort)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Auth proxy
// ---------------------------------------------------------------------------

func handleAuthConn(client net.Conn, cfg *Config) {
	defer client.Close()
	addr := client.RemoteAddr().(*net.TCPAddr)

	server, err := newDialer(cfg).Dial("tcp",
		net.JoinHostPort(cfg.AuthServerHost, strconv.Itoa(cfg.AuthServerPort)))
	if err != nil {
		log.Printf("Auth   %s: upstream unreachable: %v", addr, err)
		return
	}
	defer server.Close()

	log.Printf("Auth   %s", addr)
	var sent, recv int64
	biPipe(client, server, &sent, &recv,
		func(src, dst net.Conn, c *int64) { pipeServerToClient(src, dst, cfg, c) })

	log.Printf("Auth   %s closed sent=%d recv=%d", addr, sent, recv)
}

func startAuthProxy(cfg *Config) error {
	ln, err := net.Listen("tcp", net.JoinHostPort(cfg.ListenHost, strconv.Itoa(cfg.AuthListenPort)))
	if err != nil {
		return fmt.Errorf("auth proxy :%d: %w", cfg.AuthListenPort, err)
	}
	log.Printf("Auth   proxy %s:%d -> %s:%d",
		cfg.ListenHost, cfg.AuthListenPort, cfg.AuthServerHost, cfg.AuthServerPort)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				break
			}
			go handleAuthConn(conn, cfg)
		}
	}()
	return nil
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	cfgPath := "config.yaml"
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		log.Fatalf("Config: %v", err)
	}
	if cfg.ProxyIP == "" {
		if cfg.ListenHost == "0.0.0.0" || cfg.ListenHost == "" {
			log.Fatalln("proxy_ip is required when listen_host is 0.0.0.0")
		}
		cfg.ProxyIP = cfg.ListenHost
	}

	log.Printf("Outbound IP: %s", cfg.ProxyIP)

	if err := startWorldProxy(cfg); err != nil {
		log.Fatalf("World proxy: %v", err)
	}
	if err := startAuthProxy(cfg); err != nil {
		log.Fatalf("Auth proxy: %v", err)
	}

	select {} // block forever
}
