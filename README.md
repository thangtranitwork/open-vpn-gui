# OpenVPN Web Controller

A lightweight, self-contained, and modern web-based GUI for OpenVPN on Linux, built with **Go** and a responsive **glassmorphic frontend**. 

This application runs as a local web server (requiring root privileges to configure VPN interfaces) and allows you to manage profiles, saved credential templates, and monitor connection status and logs in real-time.

---

## 🚀 Features

- **Single Binary Portability**: Web assets (HTML, CSS, JS) are embedded directly into the Go compiled binary using `//go:embed`.
- **Modern Glassmorphic UI**: Translucent styling with a midnight dark theme, pulsing state indicators, and micro-animations.
- **Dynamic Profile Scanner**: Automatically scans for `.ovpn` files in:
  - The current working directory.
  - The home directory (`~/`).
  - The Downloads directory (`~/Downloads`).
- **Account Profile Management**: Create, update, and delete saved credential profiles (usernames and passwords) so you can reuse them across different OVPN files.
- **Real-Time Logs Monitor**: High-performance terminal console streaming raw OpenVPN stdout/stderr output in real-time using **Server-Sent Events (SSE)**.
- **Connection Diagnostics**:
  - Real-time connection state tracking (Connected, Connecting, Disconnected, Error).
  - Session uptime counter.
  - Current WAN IP Address and geographic location lookup on connection.
  - Active TUN/TAP network interface detection.
  - Click-to-copy WAN IP address helper.

---

## 🛠️ Requirements

- **Operating System**: Linux (required for OpenVPN control and interface configuration).
- **OpenVPN**: Installed and available in the system PATH (`sudo apt install openvpn`).
- **Go**: Version 1.22 or higher (only needed for compiling from source).

---

## 📦 Getting Started

### 1. Clone the repository
```bash
git clone https://github.com/thangtranitwork/open-vpn-gui.git
cd open-vpn-gui
```

### 2. Build the binary
Compile the self-contained executable:
```bash
go build -o open-vpn-gui main.go
```

### 3. Run the application
Since OpenVPN needs root permissions to configure network interfaces, manipulate routing tables, and create TUN/TAP devices, run the binary with `sudo`:
```bash
sudo ./open-vpn-gui
```

### 4. Open the Interface
Open your browser and navigate to:
```
http://127.0.0.1:5001
```
*Note: For security, the server binds exclusively to `127.0.0.1` so it is not exposed to the local network.*

---

## 🔒 Security Considerations

- **Root Privilege Requirement**: The backend web server runs as `root` because spawning OpenVPN and creating network interfaces requires root access. Binding only to localhost (`127.0.0.1`) minimizes security risks.
- **Local Credentials Store**: 
  - If you check **"Save credentials securely on server"**, credentials are saved in `credentials.json` with owner-only read/write permissions (`0600`).
  - Saved accounts profile templates are stored in `accounts.json` with `0600` permissions.
  - **Important**: These configuration files contain plain-text credentials. A `.gitignore` file is included in this repository to prevent committing them to Git.
- **Temporary Authentication File**: When initiating a connection, a temporary credentials file is created under `/tmp` with `0600` permissions (readable only by root). It is automatically wiped 5 seconds after the OpenVPN process initiates.

---

## 🗂️ Project Structure

```
├── main.go            # Go HTTP server, VPN controller & SSE broadcaster
├── web/
│   ├── index.html     # Dashboard layout & structure
│   ├── style.css      # Glassmorphic midnight styling
│   └── app.js         # REST API clients & SSE real-time log logic
├── credentials.json   # Auto-generated credentials mappings (Git-ignored)
├── accounts.json      # Auto-generated saved account templates (Git-ignored)
└── .gitignore         # Prevents committing credentials/binaries to Git
```

---

## 🔌 API Reference

| Endpoint | Method | Description |
| :--- | :--- | :--- |
| `/` | `GET` | Serves the main UI page |
| `/api/configs` | `GET` | Scans and lists available `.ovpn` configuration profiles |
| `/api/status` | `GET` | Returns active VPN connection stats & status details |
| `/api/connect` | `POST` | Triggers connection to a selected profile |
| `/api/disconnect` | `POST` | Gracefully terminates the running OpenVPN instance |
| `/api/logs` | `GET` | Streams live OpenVPN stdout logs via Server-Sent Events (SSE) |
| `/api/accounts` | `GET/POST` | Manages saved user credential profiles |
| `/api/accounts/delete` | `POST` | Deletes a saved user credential profile |
