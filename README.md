# DropRuutu

**DropRuutu** is a high-performance, single-binary web file server and manager written in **Go (Golang)**. Designed with a **Metro / Modern UI** design language inspired by classic Windows Phone and Nokia Lumia Live Tiles, featuring solid-color live tiles, an iconic circular Application Bar, and instant zero-typing QR code pairing for phones and tablets.

All web assets (HTML, CSS, templates) are compiled directly into a single standalone `.exe` binary with zero external runtime dependencies.

---

## Highlights & Key Features

### 📱 Metro / Modern UI Experience
- **Live Tile Grid**: Pitch-black background with vibrant, solid-color flat tiles categorized by file type (Mango for images, Red for videos, Magenta for audio, Blue for documents, Teal for code, Purple for archives).
- **Docked Application Bar**: Iconic circular Windows Phone-style action bar docked at the bottom of the screen (`upload`, `new folder`, `download zip`, `connect`, `root`).
- **Drag-and-Drop Organization**:
  - Drag files directly into folder tiles on the start screen to move them.
  - Drag files from your desktop directly onto any folder tile or background to upload them immediately into that directory.

### 📷 Instant Mobile & Phone Connection (QR Code)
- **Terminal QR Code**: When the server launches, it draws an ASCII QR code directly inside your terminal console. Point your phone camera at your PC screen to connect instantly without typing IP addresses or ports.
- **In-App QR Live Tile**: A dedicated Mango tile (**`connect phone`**) on the Start screen and top header opens an interactive QR modal showing the direct Wi-Fi URL and a 1-click link copy button.

### ⚡ High-Throughput & Low-Footprint Engine
- **Zero-Buffer Streaming ZIP Downloads**: Folders of any size (even hundreds of gigabytes) are compressed and piped directly to the client socket on the fly in real-time with constant $O(1)$ memory usage.
- **Direct Socket-to-Disk Uploads**: Multipart uploads stream straight to destination files in 64 KB chunks without intermediate heap buffering.
- **Resumable HTTP Range Requests (`206 Partial Content`)**: Video/audio scrubbing, partial downloads, and download resumption work out of the box.
- **Ultra-Low Resource Usage**: Idles at ~10–15 MB RAM with zero background CPU overhead.

### 🛡️ Hardened Path Traversal & Sandbox Security
All file and directory operations are validated by a multi-stage security filter (`resolveSafe`):
1. **UNC Path Rejection**: Strips and blocks UNC network paths (`\\server\share`).
2. **Volume Specifier Defense**: Rejects Windows drive letters and volume prefixes (`C:`, `D:`).
3. **Path Normalization**: Cleans and canonicalizes relative path separators using `filepath.Clean` and `filepath.FromSlash`.
4. **Boundary Confinement**: Validates that all target paths resolve strictly within the designated storage root using `filepath.Rel`.
5. **Symlink Escape Protection**: Resolves symbolic links with `filepath.EvalSymlinks` to guarantee symlinks cannot escape the sandbox.

---

## Project Structure

```text
DropRuutu/
├── static/
│   └── css/
│       └── style.css       # Metro / Modern UI stylesheet (embedded into binary)
├── templates/
│   └── index.html          # Responsive Start Screen & Live Tiles (embedded into binary)
├── go.mod                  # Go module definition
├── go.sum                  # Dependency checksums (github.com/skip2/go-qrcode)
├── main.go                 # Complete HTTP server, streaming zip, QR, and file APIs
├── main_test.go            # Comprehensive automated test suite
└── README.md
```

---

## Getting Started

### Prerequisites
- [Go](https://go.dev/) (version 1.22 or newer recommended to build from source).

### Build & Run

#### Windows
1. **Build the Standalone Binary**:
   ```powershell
   go build -ldflags="-s -w" -o DropRuutu.exe main.go
   ```
   * The `-ldflags="-s -w"` flags strip debug symbols, producing a compact, optimized standalone binary.
   * All HTML templates (`templates/index.html`) and stylesheets (`static/css/style.css`) are bundled directly into the executable via Go's native `//go:embed` directive. No external assets or dependencies are needed at runtime.

2. **Run**:
   ```powershell
   .\DropRuutu.exe
   ```

#### Linux
1. **Build the Standalone Binary**:
   ```bash
   go build -ldflags="-s -w" -o dropruutu main.go
   ```

2. **Run**:
   ```bash
   chmod +x dropruutu
   ./dropruutu
   ```

3. **Run with Custom Port / Storage**:
   ```bash
   PORT=80 UPLOAD_DIR="/srv/dropruutu" ./dropruutu
   ```

4. **Run in Background (nohup / systemd)**:
   ```bash
   nohup ./dropruutu > dropruutu.log 2>&1 &
   ```

#### Accessing the Server
- **On your PC / Server**: Open [http://localhost:3000](http://localhost:3000)
- **On your phone / tablet**: Point your camera at the QR code displayed in the terminal console, or open `http://<your-lan-ip>:3000`.

---

## Configuration

You can customize the server port and storage directory using environment variables:

| Variable | Default Value | Description |
| :--- | :--- | :--- |
| `PORT` | `3000` | Port on which the HTTP server listens. Set to `80` to drop the port number in the browser. |
| `UPLOAD_DIR` | `C:\FileServerData` (Windows)<br>`./uploads` (Linux/macOS) | Storage directory sandbox. All files and directories are securely stored here. |

### Configuration Examples

**PowerShell (Windows):**
```powershell
$env:PORT = "80"
$env:UPLOAD_DIR = "D:\MySharedFiles"
.\DropRuutu.exe
```

**Bash (Linux / macOS):**
```bash
export PORT="80"
export UPLOAD_DIR="/srv/dropruutu"
./dropruutu
```

---

## API & Endpoints

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `GET` | `/` | Serves the main Start screen UI. |
| `GET` | `/folder/{name...}` | Serves the web UI positioned inside a specific directory. |
| `GET` | `/css/style.css` | Serves the Metro / Modern UI stylesheet (cache-invalidated). |
| `GET` | `/api/folder?path=<path>` | JSON API returning items in the specified directory. |
| `GET` | `/api/info` | JSON API returning local IP, hostname, and connect URL. |
| `GET` | `/api/qr?url=<url>` | Generates a PNG QR code for the given URL (or default connect URL). |
| `POST` | `/folder` | Creates a new directory (`name` and optional `folder` in body). |
| `POST` | `/upload` | Multipart file upload endpoint supporting single and batch uploads. |
| `GET` | `/download/{path...}` | Downloads a specific file (supports HTTP range partial content). |
| `GET` | `/download-folder/{folder...}` | Streams a directory as a compressed `.zip` archive on the fly. |
| `POST` | `/delete` | Deletes a file or directory recursively (`target` in form body). |
| `POST` | `/move` | Moves a file or folder into another directory (`source` and `destination`). |

---

## Running the Automated Test Suite

The project includes unit and integration tests covering all routes, uploads, range requests, zip generation, file movements, and security traversal attacks:

```powershell
go test -v ./...
```
