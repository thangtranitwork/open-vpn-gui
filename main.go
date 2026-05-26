package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed web/index.html web/style.css web/app.js
var embedFS embed.FS

// ConfigItem represents a scanned OVPN file
type ConfigItem struct {
	Name          string `json:"name"`
	Path          string `json:"path"`
	SavedUsername string `json:"saved_username,omitempty"`
	SavedPassword string `json:"saved_password,omitempty"`
}

// SavedCredential represents credentials for a specific config path
type SavedCredential struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// SavedAccount represents a reusable VPN credential profile
type SavedAccount struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// VPNState holds the status of the connection
type VPNState struct {
	sync.Mutex
	Status       string    `json:"status"` // "disconnected", "connecting", "connected", "disconnecting", "error"
	ActiveConfig string    `json:"active_config"`
	StartTime    time.Time `json:"start_time,omitempty"`
	IPAddress    string    `json:"ip_address,omitempty"`
	Interface    string    `json:"interface,omitempty"`
	ErrorMsg     string    `json:"error_msg,omitempty"`

	cmd          *exec.Cmd
	tempAuthFile string
}

// LogBroadcaster manages SSE client channels
type LogBroadcaster struct {
	sync.Mutex
	clients map[chan string]bool
	buffer  []string
}

// Global variables
var (
	state       = &VPNState{Status: "disconnected"}
	broadcaster = &LogBroadcaster{
		clients: make(map[chan string]bool),
		buffer:  make([]string, 0, 200),
	}
	credsMutex    sync.Mutex
	accountsMutex sync.Mutex
)

const (
	credentialsFile = "credentials.json"
	accountsFile    = "accounts.json"
	defaultPort     = ":5001"
)

func main() {
	// Require root permissions since OpenVPN needs root to manipulate routing tables and create tun/tap interfaces
	if os.Geteuid() != 0 {
		fmt.Println("==================================================================")
		fmt.Println("WARNING: This program must be run with root privileges (e.g. sudo).")
		fmt.Println("OpenVPN requires root permissions to configure network interfaces.")
		fmt.Println("Please run: sudo ./open-vpn-gui")
		fmt.Println("==================================================================")
	}

	// Routes
	http.HandleFunc("/", serveIndex)
	http.HandleFunc("/static/style.css", serveCSS)
	http.HandleFunc("/static/app.js", serveJS)

	http.HandleFunc("/api/configs", handleConfigs)
	http.HandleFunc("/api/status", handleStatus)
	http.HandleFunc("/api/connect", handleConnect)
	http.HandleFunc("/api/disconnect", handleDisconnect)
	http.HandleFunc("/api/logs", handleLogs)
	http.HandleFunc("/api/accounts", handleAccounts)
	http.HandleFunc("/api/accounts/delete", handleDeleteAccount)

	log.Printf("Starting OpenVPN GUI Web Server on http://127.0.0.1%s", defaultPort)
	log.Printf("Please open this link in your browser to manage your VPN connection.")

	// Bind only to localhost for security
	err := http.ListenAndServe("127.0.0.1"+defaultPort, nil)
	if err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

// --- Serve Embedded Static Files ---

func serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := embedFS.ReadFile("web/index.html")
	if err != nil {
		http.Error(w, "Index file not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func serveCSS(w http.ResponseWriter, r *http.Request) {
	data, err := embedFS.ReadFile("web/style.css")
	if err != nil {
		http.Error(w, "CSS file not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Write(data)
}

func serveJS(w http.ResponseWriter, r *http.Request) {
	data, err := embedFS.ReadFile("web/app.js")
	if err != nil {
		http.Error(w, "JS file not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Write(data)
}

// --- API Route Handlers ---

func handleConfigs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	customDir := r.URL.Query().Get("custom_dir")

	configs, err := scanOVPNConfigs(customDir)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(configs)
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	state.Lock()
	defer state.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(state)
}

type ConnectRequest struct {
	ConfigPath      string `json:"config_path"`
	Username        string `json:"username"`
	Password        string `json:"password"`
	SaveCredentials bool   `json:"save_credentials"`
}

func handleConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ConnectRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if req.ConfigPath == "" || req.Username == "" || req.Password == "" {
		writeJSONError(w, http.StatusBadRequest, "config_path, username, and password are required")
		return
	}

	err = startVPN(req.ConfigPath, req.Username, req.Password, req.SaveCredentials)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "VPN connection process started"})
}

func handleDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := stopVPN()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "VPN disconnect process started"})
}

func handleLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := broadcaster.Register()
	defer broadcaster.Unregister(ch)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	notify := r.Context().Done()

	for {
		select {
		case <-notify:
			return
		case line := <-ch:
			_, err := fmt.Fprintf(w, "data: %s\n\n", line)
			if err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// --- VPN Helper Functions ---

func scanOVPNConfigs(customDir string) ([]ConfigItem, error) {
	var configs []ConfigItem

	if customDir != "" {
		dir := customDir
		if strings.HasPrefix(dir, "~") {
			var homeDir string
			if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
				homeDir = filepath.Join("/home", sudoUser)
			} else {
				var err error
				homeDir, err = os.UserHomeDir()
				if err == nil {
					dir = filepath.Join(homeDir, dir[1:])
				}
			}
		}

		info, err := os.Stat(dir)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("Directory does not exist: %s", customDir)
			}
			return nil, fmt.Errorf("Error accessing directory: %v", err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("Path is not a directory: %s", customDir)
		}

		files, err := filepath.Glob(filepath.Join(dir, "*.ovpn"))
		if err != nil {
			return nil, fmt.Errorf("Error scanning directory: %v", err)
		}
		for _, f := range files {
			abs, err := filepath.Abs(f)
			if err == nil {
				configs = append(configs, ConfigItem{Name: filepath.Base(f), Path: abs})
			}
		}
	} else {
		// 1. Scan current working directory
		files, _ := filepath.Glob("*.ovpn")
		for _, f := range files {
			abs, err := filepath.Abs(f)
			if err == nil {
				configs = append(configs, ConfigItem{Name: filepath.Base(f), Path: abs})
			}
		}

		// Get actual home directory (resolving SUDO_USER if run under sudo)
		var homeDir string
		if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
			homeDir = filepath.Join("/home", sudoUser)
		} else {
			var err error
			homeDir, err = os.UserHomeDir()
			if err != nil {
				homeDir = ""
			}
		}

		if homeDir != "" {
			// 2. Scan user's home directory
			homeFiles, _ := filepath.Glob(filepath.Join(homeDir, "*.ovpn"))
			for _, f := range homeFiles {
				configs = append(configs, ConfigItem{Name: filepath.Base(f), Path: f})
			}

			// 3. Scan user's Downloads directory
			downloadFiles, _ := filepath.Glob(filepath.Join(homeDir, "Downloads", "*.ovpn"))
			for _, f := range downloadFiles {
				configs = append(configs, ConfigItem{Name: filepath.Base(f), Path: f})
			}
		}
	}

	// Filter duplicate paths
	seen := make(map[string]bool)
	var uniqueConfigs []ConfigItem
	for _, c := range configs {
		if !seen[c.Path] {
			seen[c.Path] = true
			uniqueConfigs = append(uniqueConfigs, c)
		}
	}

	// Load credentials to pre-fill
	creds, _ := loadCredentials()
	for i, cfg := range uniqueConfigs {
		if cred, ok := creds[cfg.Path]; ok {
			uniqueConfigs[i].SavedUsername = cred.Username
			uniqueConfigs[i].SavedPassword = cred.Password
		}
	}

	return uniqueConfigs, nil
}

func loadCredentials() (map[string]SavedCredential, error) {
	credsMutex.Lock()
	defer credsMutex.Unlock()

	file, err := os.Open(credentialsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]SavedCredential), nil
		}
		return nil, err
	}
	defer file.Close()

	var creds map[string]SavedCredential
	if err := json.NewDecoder(file).Decode(&creds); err != nil {
		return nil, err
	}
	return creds, nil
}

func saveCredential(configPath, username, password string) error {
	creds, err := loadCredentials()
	if err != nil {
		return err
	}

	credsMutex.Lock()
	defer credsMutex.Unlock()

	creds[configPath] = SavedCredential{
		Username: username,
		Password: password,
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(credentialsFile, data, 0600)
}

func deleteCredential(configPath string) error {
	creds, err := loadCredentials()
	if err != nil {
		return err
	}

	credsMutex.Lock()
	defer credsMutex.Unlock()

	if _, ok := creds[configPath]; !ok {
		return nil
	}

	delete(creds, configPath)

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(credentialsFile, data, 0600)
}

func startVPN(configPath, username, password string, saveCreds bool) error {
	state.Lock()
	if state.Status == "connecting" || state.Status == "connected" {
		state.Unlock()
		return fmt.Errorf("VPN is already active or connecting")
	}

	state.Status = "connecting"
	state.ActiveConfig = configPath
	state.ErrorMsg = ""
	state.IPAddress = ""
	state.Interface = ""
	state.StartTime = time.Time{}
	state.Unlock()

	if saveCreds {
		_ = saveCredential(configPath, username, password)
	} else {
		_ = deleteCredential(configPath)
	}

	// Create a temporary file with credentials
	tmpFile, err := os.CreateTemp("", "vpn-auth-*.tmp")
	if err != nil {
		updateStateError(fmt.Sprintf("Failed to create credentials temp file: %v", err))
		return err
	}
	_ = tmpFile.Chmod(0600) // Restricted to owner (root)

	_, err = tmpFile.WriteString(username + "\n" + password + "\n")
	tmpFile.Close()
	if err != nil {
		os.Remove(tmpFile.Name())
		updateStateError(fmt.Sprintf("Failed to write credentials: %v", err))
		return err
	}

	state.Lock()
	state.tempAuthFile = tmpFile.Name()

	// Launch OpenVPN
	// Note: We direct logs to stdout/stderr and parse them
	cmd := exec.Command("openvpn", "--config", configPath, "--auth-user-pass", tmpFile.Name(), "--disable-dco")
	state.cmd = cmd
	state.Unlock()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cleanupTempAuth()
		updateStateError(fmt.Sprintf("Failed to redirect stdout: %v", err))
		return err
	}
	cmd.Stderr = cmd.Stdout // merge stderr into stdout

	if err := cmd.Start(); err != nil {
		cleanupTempAuth()
		updateStateError(fmt.Sprintf("Failed to launch openvpn process: %v. Make sure 'openvpn' is installed and this app is run with sudo.", err))
		return err
	}

	// Delete credentials file after a short delay (once OpenVPN has parsed arguments and started negotiations)
	go func() {
		time.Sleep(5 * time.Second)
		cleanupTempAuth()
	}()

	// Clear logs console buffer
	broadcaster.Clear()
	broadcaster.Broadcast("SYSTEM: Launching OpenVPN process...")

	// Listen to stdout/stderr in a separate goroutine
	go func() {
		reader := io.Reader(stdout)
		buf := make([]byte, 1024)
		var lineAccumulator string

		for {
			n, err := reader.Read(buf)
			if n > 0 {
				lineAccumulator += string(buf[:n])
				for {
					idx := strings.Index(lineAccumulator, "\n")
					if idx == -1 {
						break
					}
					line := strings.TrimRight(lineAccumulator[:idx], "\r")
					lineAccumulator = lineAccumulator[idx+1:]

					broadcaster.Broadcast(line)
					parseLogLine(line)
				}
			}
			if err != nil {
				break
			}
		}

		// Wait for process to exit
		err = cmd.Wait()

		state.Lock()
		if state.Status == "connecting" || state.Status == "connected" {
			state.Status = "error"
			if err != nil {
				state.ErrorMsg = fmt.Sprintf("VPN process exited: %v", err)
			} else {
				state.ErrorMsg = "VPN process exited unexpectedly"
			}
			broadcaster.Broadcast(fmt.Sprintf("SYSTEM: Connection failed or exited. Error: %s", state.ErrorMsg))
		} else if state.Status == "disconnecting" {
			state.Status = "disconnected"
			broadcaster.Broadcast("SYSTEM: Disconnected.")
		}
		state.cmd = nil
		state.Unlock()

		cleanupTempAuth()
	}()

	return nil
}

func stopVPN() error {
	state.Lock()
	defer state.Unlock()

	if state.cmd == nil {
		state.Status = "disconnected"
		return nil
	}

	state.Status = "disconnecting"
	broadcaster.Broadcast("SYSTEM: Disconnect signal sent...")

	// SIGTERM lets OpenVPN gracefully drop routes and close devices
	err := state.cmd.Process.Signal(syscall.SIGTERM)
	if err != nil {
		log.Printf("Failed to SIGTERM VPN process: %v, falling back to Kill", err)
		err = state.cmd.Process.Kill()
	}

	return err
}

func parseLogLine(line string) {
	state.Lock()
	defer state.Unlock()

	// Match: "Initialization Sequence Completed"
	if strings.Contains(line, "Initialization Sequence Completed") {
		state.Status = "connected"
		state.StartTime = time.Now()
		state.ErrorMsg = ""
		broadcaster.Broadcast("SYSTEM: Successfully connected! Fetching new WAN IP...")

		// Async fetch of WAN IP
		go func() {
			time.Sleep(2 * time.Second) // wait for routing to adjust
			ip, location := fetchCurrentIP()

			state.Lock()
			if state.Status == "connected" {
				state.IPAddress = ip
				if location != "" {
					state.IPAddress = fmt.Sprintf("%s (%s)", ip, location)
				}
				broadcaster.Broadcast(fmt.Sprintf("SYSTEM: Verified VPN IP address: %s", state.IPAddress))
			}
			state.Unlock()
		}()
	}

	// Match interface assignment
	// E.g.: "TUN/TAP device tun0 opened" or similar strings
	if strings.Contains(line, "TUN/TAP device") && strings.Contains(line, "opened") {
		parts := strings.Split(line, "TUN/TAP device")
		if len(parts) > 1 {
			rest := strings.TrimSpace(parts[1])
			words := strings.Fields(rest)
			if len(words) > 0 {
				dev := strings.Trim(words[0], "[]")
				state.Interface = dev
			}
		}
	}

	// Match standard error modes
	if strings.Contains(line, "AUTH_FAILED") {
		state.Status = "error"
		state.ErrorMsg = "Authentication Failed: Incorrect Username or Password"
	} else if strings.Contains(line, "TLS Error: TLS key negotiation failed") {
		state.Status = "error"
		state.ErrorMsg = "TLS Negotiation Failed: Handshake timed out"
	} else if strings.Contains(line, "Cannot resolve host address") {
		state.Status = "error"
		state.ErrorMsg = "DNS Error: Cannot resolve VPN server address"
	}
}

func fetchCurrentIP() (string, string) {
	client := http.Client{Timeout: 3 * time.Second}

	// Query ip-api for both IP and city/country
	resp, err := client.Get("http://ip-api.com/json/")
	if err != nil {
		// Fallback to ipify.org
		resp2, err2 := client.Get("https://api.ipify.org?format=text")
		if err2 != nil {
			return "Unknown", ""
		}
		defer resp2.Body.Close()
		ipBytes, _ := io.ReadAll(resp2.Body)
		return string(ipBytes), ""
	}
	defer resp.Body.Close()

	var res struct {
		Query   string `json:"query"`
		City    string `json:"city"`
		Country string `json:"country"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "Unknown", ""
	}

	if res.Query == "" {
		return "Unknown", ""
	}

	return res.Query, fmt.Sprintf("%s, %s", res.City, res.Country)
}

func cleanupTempAuth() {
	state.Lock()
	defer state.Unlock()
	if state.tempAuthFile != "" {
		os.Remove(state.tempAuthFile)
		state.tempAuthFile = ""
	}
}

func updateStateError(errMsg string) {
	state.Lock()
	defer state.Unlock()
	state.Status = "error"
	state.ErrorMsg = errMsg
	broadcaster.Broadcast(fmt.Sprintf("SYSTEM ERROR: %s", errMsg))
}

// --- Log Broadcaster Helper Methods ---

func (lb *LogBroadcaster) Register() chan string {
	lb.Lock()
	defer lb.Unlock()
	ch := make(chan string, 100)
	lb.clients[ch] = true
	for _, line := range lb.buffer {
		ch <- line
	}
	return ch
}

func (lb *LogBroadcaster) Unregister(ch chan string) {
	lb.Lock()
	defer lb.Unlock()
	delete(lb.clients, ch)
	close(ch)
}

func (lb *LogBroadcaster) Broadcast(line string) {
	lb.Lock()
	defer lb.Unlock()

	if len(lb.buffer) >= 200 {
		lb.buffer = lb.buffer[1:]
	}
	lb.buffer = append(lb.buffer, line)

	for ch := range lb.clients {
		select {
		case ch <- line:
		default:
			// slow consumer, skip
		}
	}
}

func (lb *LogBroadcaster) Clear() {
	lb.Lock()
	defer lb.Unlock()
	lb.buffer = lb.buffer[:0]
}

// --- HTTP Helpers ---

func writeJSONError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// --- Accounts Management Helper Functions ---

func loadAccounts() ([]SavedAccount, error) {
	accountsMutex.Lock()
	defer accountsMutex.Unlock()

	file, err := os.Open(accountsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return []SavedAccount{}, nil
		}
		return nil, err
	}
	defer file.Close()

	var accounts []SavedAccount
	if err := json.NewDecoder(file).Decode(&accounts); err != nil {
		return nil, err
	}
	return accounts, nil
}

func saveAccounts(accounts []SavedAccount) error {
	accountsMutex.Lock()
	defer accountsMutex.Unlock()

	data, err := json.MarshalIndent(accounts, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(accountsFile, data, 0600)
}

func handleAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		accounts, err := loadAccounts()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(accounts)
		return
	}

	if r.Method == http.MethodPost {
		var req SavedAccount
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "Invalid payload")
			return
		}

		if req.Username == "" || req.Password == "" || req.Label == "" {
			writeJSONError(w, http.StatusBadRequest, "Label, username, and password are required")
			return
		}

		accounts, err := loadAccounts()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// If ID is empty, generate a unique ID. Otherwise, update.
		if req.ID == "" {
			req.ID = fmt.Sprintf("acc_%d", time.Now().UnixNano())
			accounts = append(accounts, req)
		} else {
			found := false
			for i, acc := range accounts {
				if acc.ID == req.ID {
					accounts[i] = req
					found = true
					break
				}
			}
			if !found {
				accounts = append(accounts, req)
			}
		}

		if err := saveAccounts(accounts); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(accounts)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid payload")
		return
	}

	if req.ID == "" {
		writeJSONError(w, http.StatusBadRequest, "ID is required")
		return
	}

	accounts, err := loadAccounts()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var updated []SavedAccount
	for _, acc := range accounts {
		if acc.ID != req.ID {
			updated = append(updated, acc)
		}
	}

	if err := saveAccounts(updated); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updated)
}
