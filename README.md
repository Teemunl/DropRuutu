# DropRuutu

DropRuutu is a Go web file server for uploading, browsing, moving, and downloading files from a browser. Its web interface and assets are embedded in the executable.

## Build and run

Requires Go 1.22 or newer.
## Windows
```Windows
go build -o DropRuutu.exe .
.\DropRuutu.exe
```
## Linux
```Linux
go build -o dropruutu .
./dropruutu
```

Open [http://localhost:3000](http://localhost:3000). To connect from a phone or another device on your local network, scan the QR code shown in the terminal or open the server's LAN address.

## Configuration

| Environment variable | Default | Description |
| --- | --- | --- |
| `PORT` | `3000` | Port for the web server. |
| `UPLOAD_DIR` | `C:\FileServerData` on Windows; `./uploads` on Linux/macOS | Directory used to store files. |

PowerShell example:

```powershell
$env:PORT = "8080"
$env:UPLOAD_DIR = "D:\SharedFiles"
.\DropRuutu.exe
```

## Features

- Browser-based file manager with drag-and-drop uploads.
- QR code for connecting from devices on the local network.
- File and folder upload, download, move, delete, and ZIP download.
- HTTP range requests for partial and resumable downloads.
- Path validation to keep file operations inside the configured storage directory.

## API

| Method | Endpoint | Description |
| --- | --- | --- |
| `GET` | `/` | File manager interface |
| `GET` | `/folder/{path...}` | Interface for a folder |
| `GET` | `/api/folder?path=<path>` | List folder contents |
| `GET` | `/api/info` | Server and connection information |
| `GET` | `/api/qr?url=<url>` | Generate a QR code |
| `POST` | `/folder` | Create a folder |
| `POST` | `/upload` | Upload files |
| `GET` | `/download/{path...}` | Download a file |
| `GET` | `/download-folder/{path...}` | Download a folder as ZIP |
| `POST` | `/delete` | Delete a file or folder |
| `POST` | `/move` | Move a file or folder |

## Tests

```powershell
go test ./...
```

<p align="center">
  <img src="images/dropruutu.png" alt="DropRuutu logo" width="160">
</p>
