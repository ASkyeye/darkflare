// Copyright (c) Barrett Lyon
// blyon@blyon.com
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package main

import (
	"bufio"
	"crypto/tls"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type Session struct {
	conn       net.Conn
	lastActive time.Time
	buffer     []byte
	mu         sync.Mutex
}

type Server struct {
	sessions    sync.Map
	destHost    string
	destPort    string
	debug       bool
	appCommand  string
	isAppMode   bool
	allowDirect bool
}

func NewServer(destHost, destPort string, appCommand string, debug bool, allowDirect bool) *Server {
	s := &Server{
		destHost:    destHost,
		destPort:    destPort,
		debug:       debug,
		appCommand:  appCommand,
		isAppMode:   appCommand != "",
		allowDirect: allowDirect,
	}

	if s.isAppMode && s.debug {
		log.Printf("Starting in application mode with command: %s", appCommand)
	}

	go s.cleanupSessions()
	return s
}

func (s *Server) cleanupSessions() {
	for {
		time.Sleep(time.Minute)
		now := time.Now()
		s.sessions.Range(func(key, value interface{}) bool {
			session := value.(*Session)
			session.mu.Lock()
			if now.Sub(session.lastActive) > 5*time.Minute {
				session.conn.Close()
				s.sessions.Delete(key)
			}
			session.mu.Unlock()
			return true
		})
	}
}

func (s *Server) handleApplication(w http.ResponseWriter, r *http.Request) {
	if s.debug {
		log.Printf("Handling application request from %s", r.Header.Get("Cf-Connecting-Ip"))
	}

	parts := strings.Fields(s.appCommand)
	if len(parts) == 0 {
		http.Error(w, "Invalid application command", http.StatusInternalServerError)
		return
	}

	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Env = os.Environ()

	if s.debug {
		log.Printf("Launching application: %s", s.appCommand)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("Failed to create stdout pipe: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Printf("Failed to create stderr pipe: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := cmd.Start(); err != nil {
		log.Printf("Failed to start application: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Handle stdout in a goroutine
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if s.debug {
				log.Printf("Application stdout: %s", scanner.Text())
			}
		}
		if err := scanner.Err(); err != nil && s.debug {
			log.Printf("Error reading stdout: %v", err)
		}
	}()

	// Handle stderr in a goroutine
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			if s.debug {
				log.Printf("Application stderr: %s", scanner.Text())
			}
		}
		if err := scanner.Err(); err != nil && s.debug {
			log.Printf("Error reading stderr: %v", err)
		}
	}()

	if err := cmd.Wait(); err != nil {
		if s.debug {
			log.Printf("Application exited with error: %v", err)
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	if s.isAppMode {
		s.handleApplication(w, r)
		return
	}

	if s.debug {
		log.Printf("Request: %s %s from %s",
			r.Method,
			r.URL.Path,
			r.Header.Get("Cf-Connecting-Ip"),
		)
		log.Printf("Headers: %+v", r.Header)
	}

	// Verify Cloudflare connection
	cfConnecting := r.Header.Get("Cf-Connecting-Ip")
	if cfConnecting == "" && !s.allowDirect {
		http.Error(w, "Direct access not allowed", http.StatusForbidden)
		return
	}

	// Set Apache-like headers
	w.Header().Set("Server", "Apache/2.4.41 (Ubuntu)")
	w.Header().Set("X-Powered-By", "PHP/7.4.33")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("X-XSS-Protection", "1; mode=block")

	// Cache control headers
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Content-Type", "application/octet-stream")

	// Try to get session ID from various possible headers
	sessionID := r.Header.Get("X-Ephemeral")
	if sessionID == "" {
		// Try Cloudflare-specific headers
		sessionID = r.Header.Get("Cf-Ray")
		if sessionID == "" {
			// Could also try other headers or generate a session ID based on IP
			sessionID = r.Header.Get("Cf-Connecting-Ip")
		}
	}

	if sessionID == "" {
		if s.debug {
			log.Printf("Error: Missing session ID from %s", r.Header.Get("Cf-Connecting-Ip"))
		}
		http.Error(w, "Missing session ID", http.StatusBadRequest)
		return
	}

	var session *Session
	sessionInterface, exists := s.sessions.Load(sessionID)
	if !exists {
		conn, err := net.Dial("tcp", fmt.Sprintf("%s:%s", s.destHost, s.destPort))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		session = &Session{
			conn:       conn,
			lastActive: time.Now(),
			buffer:     make([]byte, 0),
		}
		s.sessions.Store(sessionID, session)
	} else {
		session = sessionInterface.(*Session)
	}

	session.mu.Lock()
	defer session.mu.Unlock()
	session.lastActive = time.Now()

	if r.Method == http.MethodPost {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			if s.debug {
				log.Printf("Error reading request body: %v", err)
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if len(data) > 0 {
			if s.debug {
				log.Printf("POST: Writing %d bytes to connection for session %s",
					len(data),
					sessionID[:8], // First 8 chars of session ID for brevity
				)
			}
			_, err = session.conn.Write(data)
			if err != nil {
				if s.debug {
					log.Printf("Error writing to connection: %v", err)
				}
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		return
	}

	// For GET requests, read any available data
	buffer := make([]byte, 8192)
	var readData []byte

	for {
		session.conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		n, err := session.conn.Read(buffer)
		if err != nil {
			if err != io.EOF && !err.(net.Error).Timeout() {
				if s.debug {
					log.Printf("Error reading from connection: %v", err)
				}
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			break
		}
		if n > 0 {
			if s.debug {
				log.Printf("GET: Read %d bytes from connection for session %s",
					n,
					sessionID[:8],
				)
			}
			readData = append(readData, buffer[:n]...)
		}
		if n < len(buffer) {
			break
		}
	}

	// Only encode and send if we have data
	if len(readData) > 0 {
		encoded := hex.EncodeToString(readData)
		if s.debug {
			log.Printf("Response: Sending %d bytes (encoded: %d bytes) for session %s path %s",
				len(readData),
				len(encoded),
				sessionID[:8],
				r.URL.Path,
			)
		}
		w.Write([]byte(encoded))
	} else if s.debug {
		log.Printf("Response: No data to send for session %s path %s",
			sessionID[:8],
			r.URL.Path,
		)
	}
}

func main() {
	var origin string
	var dest string
	var certFile string
	var keyFile string
	var debug bool
	var allowDirect bool
	var appCommand string

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "DarkFlare Server - TCP-over-CDN tunnel server component\n")
		fmt.Fprintf(os.Stderr, "(c) 2024 Barrett Lyon - blyon@blyon.com\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		fmt.Fprintf(os.Stderr, "  -o        Origin address in format: http(s)://ip:port\n")
		fmt.Fprintf(os.Stderr, "            Example: https://0.0.0.0:443\n")
		fmt.Fprintf(os.Stderr, "  -c        Path to certificate file (required for HTTPS)\n")
		fmt.Fprintf(os.Stderr, "  -k        Path to private key file (required for HTTPS)\n")
		fmt.Fprintf(os.Stderr, "  -d        Destination address in host:port format\n")
		fmt.Fprintf(os.Stderr, "            Example: localhost:22 for SSH forwarding\n\n")
		fmt.Fprintf(os.Stderr, "  -a        Application mode: launches a command instead of forwarding\n")
		fmt.Fprintf(os.Stderr, "            Example: 'sshd -i' or 'pppd noauth'\n")
		fmt.Fprintf(os.Stderr, "            Note: Cannot be used with -d flag\n\n")
		fmt.Fprintf(os.Stderr, "  -debug    Enable debug logging\n")
		fmt.Fprintf(os.Stderr, "  -allow-direct  Allow direct connections without Cloudflare headers\n")
		fmt.Fprintf(os.Stderr, "            Warning: Not recommended for production use\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  HTTPS Server:\n")
		fmt.Fprintf(os.Stderr, "    %s -o https://0.0.0.0:443 -d localhost:22 -c /path/to/cert.pem -k /path/to/key.pem\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  HTTP Server:\n")
		fmt.Fprintf(os.Stderr, "    %s -o http://0.0.0.0:80 -d localhost:22\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "For more information: https://github.com/blyon/darkflare\n")
	}

	flag.StringVar(&origin, "o", "http://0.0.0.0:8080", "")
	flag.StringVar(&dest, "d", "", "")
	flag.StringVar(&certFile, "c", "", "")
	flag.StringVar(&keyFile, "k", "", "")
	flag.StringVar(&appCommand, "a", "", "")
	flag.BoolVar(&debug, "debug", false, "")
	flag.BoolVar(&allowDirect, "allow-direct", false, "")
	flag.Parse()

	// Parse origin URL
	originURL, err := url.Parse(origin)
	if err != nil {
		log.Fatalf("Invalid origin URL: %v", err)
	}

	// Validate scheme
	if originURL.Scheme != "http" && originURL.Scheme != "https" {
		log.Fatal("Origin scheme must be either 'http' or 'https'")
	}

	// Validate and extract host/port
	originHost, originPort, err := net.SplitHostPort(originURL.Host)
	if err != nil {
		log.Fatalf("Invalid origin address: %v", err)
	}

	// Parse destination
	var destHost, destPort string
	if dest != "" {
		destHost, destPort, err = net.SplitHostPort(dest)
		if err != nil {
			log.Fatalf("Invalid destination address: %v", err)
		}
	}

	// Validate IP is local
	if !isLocalIP(originHost) {
		log.Fatal("Origin host must be a local IP address")
	}

	server := NewServer(destHost, destPort, appCommand, debug, allowDirect)

	log.Printf("DarkFlare server running on %s://%s:%s", originURL.Scheme, originHost, originPort)
	if allowDirect {
		log.Printf("Warning: Direct connections allowed (no Cloudflare required)")
	}

	// Start server with appropriate protocol
	if originURL.Scheme == "https" {
		if certFile == "" || keyFile == "" {
			log.Fatal("HTTPS requires both certificate (-c) and key (-k) files")
		}

		// Load and verify certificates
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			log.Fatalf("Failed to load certificate and key: %v", err)
		}

		if debug {
			log.Printf("Successfully loaded certificate from %s and key from %s", certFile, keyFile)
		}

		server := &http.Server{
			Addr:    fmt.Sprintf("%s:%s", originHost, originPort),
			Handler: http.HandlerFunc(server.handleRequest),
			TLSConfig: &tls.Config{
				Certificates: []tls.Certificate{cert},
				MinVersion:   tls.VersionTLS12,
				MaxVersion:   tls.VersionTLS13,
				// Allow any cipher suites
				CipherSuites: nil,
				// Don't verify client certs
				ClientAuth: tls.NoClientCert,
				// Handle SNI
				GetCertificate: func(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
					if debug {
						log.Printf("Client requesting certificate for server name: %s", info.ServerName)
					}
					return &cert, nil
				},
				GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
					if debug {
						log.Printf("TLS Handshake Details:")
						log.Printf("  Client Address: %s", hello.Conn.RemoteAddr())
						log.Printf("  Server Name: %s", hello.ServerName)
						log.Printf("  Supported Versions: %v", hello.SupportedVersions)
						log.Printf("  Supported Ciphers: %v", hello.CipherSuites)
						log.Printf("  Supported Curves: %v", hello.SupportedCurves)
						log.Printf("  Supported Points: %v", hello.SupportedPoints)
						log.Printf("  ALPN Protocols: %v", hello.SupportedProtos)
					}
					return nil, nil
				},
				VerifyConnection: func(cs tls.ConnectionState) error {
					if debug {
						log.Printf("TLS Connection State:")
						log.Printf("  Version: 0x%x", cs.Version)
						log.Printf("  HandshakeComplete: %v", cs.HandshakeComplete)
						log.Printf("  CipherSuite: 0x%x", cs.CipherSuite)
						log.Printf("  NegotiatedProtocol: %s", cs.NegotiatedProtocol)
						log.Printf("  ServerName: %s", cs.ServerName)
					}
					return nil
				},
				// Enable HTTP/2 support
				NextProtos: []string{"h2", "http/1.1"},
			},
			ErrorLog: log.New(os.Stderr, "[HTTPS] ", log.LstdFlags),
			ConnState: func(conn net.Conn, state http.ConnState) {
				if debug {
					log.Printf("Connection state changed to %s from %s",
						state, conn.RemoteAddr().String())
				}
			},
		}

		log.Printf("Starting HTTPS server on %s:%s", originHost, originPort)
		if debug {
			log.Printf("TLS Configuration:")
			log.Printf("  Minimum Version: %x", server.TLSConfig.MinVersion)
			log.Printf("  Maximum Version: %x", server.TLSConfig.MaxVersion)
			log.Printf("  Certificates Loaded: %d", len(server.TLSConfig.Certificates))
			log.Printf("  Listening Address: %s", server.Addr)
			log.Printf("  Supported Protocols: %v", server.TLSConfig.NextProtos)
		}

		log.Fatal(server.ListenAndServeTLS(certFile, keyFile))
	} else {
		server := &http.Server{
			Addr:    fmt.Sprintf("%s:%s", originHost, originPort),
			Handler: http.HandlerFunc(server.handleRequest),
		}
		log.Fatal(server.ListenAndServe())
	}
}

func isLocalIP(ip string) bool {
	if ip == "0.0.0.0" || ip == "127.0.0.1" || ip == "::1" {
		return true
	}

	ipAddr := net.ParseIP(ip)
	if ipAddr == nil {
		return false
	}

	return ipAddr.IsLoopback() || ipAddr.IsPrivate()
}
