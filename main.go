package main

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"
)

//go:embed templates/index.html
var indexHTML []byte

//go:embed static/css/style.css
var styleCSS []byte

var (
	uploadDir = "./uploads"
	port      = "3000"
)

type FileItem struct {
	Name        string `json:"name"`
	IsDirectory bool   `json:"isDirectory"`
}

type APIResponse struct {
	Items []FileItem `json:"items"`
	Path  string     `json:"path"`
}

func init() {
	if runtime.GOOS == "windows" {
		uploadDir = `C:\FileServerData`
	}
	if envPort := os.Getenv("PORT"); envPort != "" {
		port = envPort
	}
	if envDir := os.Getenv("UPLOAD_DIR"); envDir != "" {
		uploadDir = envDir
	}
}

// resolveSafe safely maps an untrusted user-supplied path to an absolute path within uploadDir.
// It prevents directory traversal attacks (e.g. "../", absolute paths, symlink escapes).
func resolveSafe(relPath string) (string, error) {
	raw := strings.TrimSpace(relPath)
	// Immediately reject UNC network shares or drive-qualified paths
	if strings.HasPrefix(raw, `\\`) || strings.HasPrefix(raw, `//`) || filepath.VolumeName(raw) != "" {
		return "", errors.New("invalid path: absolute or network path detected")
	}

	// Clean the incoming relative path and trim leading slashes
	trimmed := strings.TrimLeft(raw, "/\\")
	cleaned := filepath.Clean(filepath.FromSlash(trimmed))
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") || filepath.VolumeName(cleaned) != "" {
		return "", errors.New("invalid path: traversal detected")
	}

	absBase, err := filepath.Abs(uploadDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve base upload dir: %w", err)
	}

	target := filepath.Join(absBase, cleaned)

	// Double-check the resolved target starts with the base directory
	rel, err := filepath.Rel(absBase, target)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", errors.New("access denied: path outside upload directory")
	}

	// If the file/folder exists on disk, verify symlink resolution stays inside absBase
	if realTarget, err := filepath.EvalSymlinks(target); err == nil {
		realBase, errBase := filepath.EvalSymlinks(absBase)
		if errBase == nil {
			relSym, errRel := filepath.Rel(realBase, realTarget)
			if errRel != nil || strings.HasPrefix(relSym, "..") {
				return "", errors.New("access denied: symlink escape detected")
			}
		}
	}

	return target, nil
}

func main() {
	// Ensure upload directory exists
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		log.Fatalf("Failed to create upload directory %s: %v", uploadDir, err)
	}

	mux := http.NewServeMux()

	// Static CSS endpoint
	mux.HandleFunc("GET /css/style.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		w.Write(styleCSS)
	})

	// Root UI & folder UI view
	mux.HandleFunc("GET /", handleIndex)
	mux.HandleFunc("GET /folder/{name...}", handleFolderView)

	// API for dynamic asynchronous folder exploration
	mux.HandleFunc("GET /api/folder", handleAPIFolder)
	mux.HandleFunc("GET /api/info", handleAPIInfo)
	mux.HandleFunc("GET /api/qr", handleQRCode)

	// Actions
	mux.HandleFunc("POST /folder", handleCreateFolder)
	mux.HandleFunc("POST /upload", handleUpload)
	mux.HandleFunc("GET /download/{path...}", handleDownloadFile)
	mux.HandleFunc("GET /download-folder/{folder...}", handleDownloadFolderZip)
	mux.HandleFunc("POST /delete", handleDelete)
	mux.HandleFunc("POST /move", handleMove)

	addr := ":" + port
	hostname, _ := os.Hostname()
	localIP := getLocalIP()

	networkURL := fmt.Sprintf("http://localhost:%s", port)
	if localIP != "" {
		if port == "80" {
			networkURL = fmt.Sprintf("http://%s", localIP)
		} else {
			networkURL = fmt.Sprintf("http://%s:%s", localIP, port)
		}
	} else if hostname != "" {
		if port == "80" {
			networkURL = fmt.Sprintf("http://%s.local", strings.ToLower(hostname))
		} else {
			networkURL = fmt.Sprintf("http://%s.local:%s", strings.ToLower(hostname), port)
		}
	}

	log.Printf("==================================================")
	log.Printf("  DropRuutu is running on port %s", port)
	log.Printf("  Storage directory: %s", uploadDir)
	log.Printf("  PC Local:       http://localhost:%s", port)
	if hostname != "" {
		log.Printf("  Phone / Wi-Fi:  http://%s.local:%s", strings.ToLower(hostname), port)
	}
	if localIP != "" {
		log.Printf("  Network URL:    %s", networkURL)
	}
	log.Printf("==================================================")

	if qr, err := qrcode.New(networkURL, qrcode.Medium); err == nil {
		fmt.Printf("\nScan with phone camera to connect:\n")
		fmt.Println(qr.ToSmallString(false))
		fmt.Println()
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second, // Protect against slowloris attacks while allowing unbounded file transfer time
		IdleTimeout:       2 * time.Minute,
	}

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}

func getLocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	localAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return ""
	}
	return localAddr.IP.String()
}

func handleAPIInfo(w http.ResponseWriter, r *http.Request) {
	localIP := getLocalIP()
	hostname, _ := os.Hostname()
	networkURL := fmt.Sprintf("http://localhost:%s", port)
	if localIP != "" {
		if port == "80" {
			networkURL = fmt.Sprintf("http://%s", localIP)
		} else {
			networkURL = fmt.Sprintf("http://%s:%s", localIP, port)
		}
	} else if hostname != "" {
		if port == "80" {
			networkURL = fmt.Sprintf("http://%s.local", strings.ToLower(hostname))
		} else {
			networkURL = fmt.Sprintf("http://%s.local:%s", strings.ToLower(hostname), port)
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	json.NewEncoder(w).Encode(map[string]string{
		"networkUrl": networkURL,
		"localIp":    localIP,
		"hostname":   strings.ToLower(hostname),
		"port":       port,
	})
}

func handleQRCode(w http.ResponseWriter, r *http.Request) {
	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		localIP := getLocalIP()
		hostname, _ := os.Hostname()
		if localIP != "" {
			if port == "80" {
				targetURL = fmt.Sprintf("http://%s", localIP)
			} else {
				targetURL = fmt.Sprintf("http://%s:%s", localIP, port)
			}
		} else if hostname != "" {
			if port == "80" {
				targetURL = fmt.Sprintf("http://%s.local", strings.ToLower(hostname))
			} else {
				targetURL = fmt.Sprintf("http://%s.local:%s", strings.ToLower(hostname), port)
			}
		} else {
			targetURL = fmt.Sprintf("http://localhost:%s", port)
		}
	}

	png, err := qrcode.Encode(targetURL, qrcode.Medium, 280)
	if err != nil {
		http.Error(w, "Failed to generate QR code", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Write(png)
}

// handleIndex serves the embedded main HTML UI
func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && !strings.HasPrefix(r.URL.Path, "/folder/") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Write(indexHTML)
}

func handleFolderView(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Write(indexHTML)
}

// handleAPIFolder returns JSON folder contents for dynamic front-end navigation
func handleAPIFolder(w http.ResponseWriter, r *http.Request) {
	relPath := r.URL.Query().Get("path")
	safePath, err := resolveSafe(relPath)
	if err != nil {
		http.Error(w, `{"error":"Invalid folder path"}`, http.StatusBadRequest)
		return
	}

	fi, err := os.Stat(safePath)
	if err != nil || !fi.IsDir() {
		http.Error(w, `{"error":"Folder not found"}`, http.StatusNotFound)
		return
	}

	entries, err := os.ReadDir(safePath)
	if err != nil {
		http.Error(w, `{"error":"Unable to read directory"}`, http.StatusInternalServerError)
		return
	}

	items := make([]FileItem, 0, len(entries))
	for _, entry := range entries {
		items = append(items, FileItem{
			Name:        entry.Name(),
			IsDirectory: entry.IsDir(),
		})
	}

	// Sort folders first, then files alphabetically
	sort.Slice(items, func(i, j int) bool {
		if items[i].IsDirectory != items[j].IsDirectory {
			return items[i].IsDirectory
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(APIResponse{
		Items: items,
		Path:  filepath.ToSlash(relPath),
	})
}

// handleCreateFolder creates a new subdirectory inside the storage
func handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		r.ParseForm()
	}

	name := r.FormValue("name")
	if strings.TrimSpace(name) == "" {
		http.Error(w, "Folder name required", http.StatusBadRequest)
		return
	}

	safePath, err := resolveSafe(name)
	if err != nil {
		http.Error(w, "Invalid folder name", http.StatusBadRequest)
		return
	}

	if err := os.MkdirAll(safePath, 0755); err != nil {
		http.Error(w, "Failed to create folder", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" || strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleUpload handles file uploads with real-time direct streaming (constant memory & zero temp disk buffering)
func handleUpload(w http.ResponseWriter, r *http.Request) {
	targetFolder := strings.TrimSpace(r.URL.Query().Get("folder"))

	destFolder, err := resolveSafe(targetFolder)
	if err != nil {
		http.Error(w, "Invalid target folder", http.StatusBadRequest)
		return
	}

	// Ensure destination directory exists
	if err := os.MkdirAll(destFolder, 0755); err != nil {
		http.Error(w, "Target directory cannot be created", http.StatusInternalServerError)
		return
	}

	mr, err := r.MultipartReader()
	if err != nil {
		// Fallback to standard multipart form if not readable as a multipart stream
		if parseErr := r.ParseMultipartForm(32 << 20); parseErr != nil {
			http.Error(w, fmt.Sprintf("Error parsing upload: %v", parseErr), http.StatusBadRequest)
			return
		}
		defer func() {
			if r.MultipartForm != nil {
				r.MultipartForm.RemoveAll()
			}
		}()

		if targetFolder == "" {
			targetFolder = strings.TrimSpace(r.FormValue("folder"))
			destFolder, _ = resolveSafe(targetFolder)
		}

		files := r.MultipartForm.File["file"]
		if len(files) == 0 {
			http.Error(w, "No files uploaded", http.StatusBadRequest)
			return
		}

		for _, fileHeader := range files {
			fileName := filepath.Base(fileHeader.Filename)
			if fileName == "." || fileName == "/" || fileName == "\\" || fileName == "" {
				continue
			}

			src, openErr := fileHeader.Open()
			if openErr != nil {
				http.Error(w, fmt.Sprintf("Failed to read uploaded file: %v", openErr), http.StatusInternalServerError)
				return
			}

			destPath := filepath.Join(destFolder, fileName)
			dst, createErr := os.Create(destPath)
			if createErr != nil {
				src.Close()
				http.Error(w, fmt.Sprintf("Failed to create destination file: %v", createErr), http.StatusInternalServerError)
				return
			}

			buf := make([]byte, 1024*1024) // 1MB buffer for fast high-throughput disk I/O
			_, copyErr := io.CopyBuffer(dst, src, buf)
			src.Close()
			dst.Close()

			if copyErr != nil {
				os.Remove(destPath)
				http.Error(w, fmt.Sprintf("Error saving file %s: %v", fileName, copyErr), http.StatusInternalServerError)
				return
			}
		}
	} else {
		// Direct streaming parser via MultipartReader
		buf := make([]byte, 1024*1024) // 1MB buffer
		uploadedCount := 0
		var writtenFiles []string
		initialDestFolder := destFolder

		for {
			part, nextErr := mr.NextPart()
			if nextErr == io.EOF {
				break
			}
			if nextErr != nil {
				http.Error(w, fmt.Sprintf("Error reading upload stream: %v", nextErr), http.StatusBadRequest)
				return
			}

			formName := part.FormName()
			fileName := part.FileName()

			if formName == "folder" && fileName == "" {
				// Destination folder passed as form field
				var valBuf bytes.Buffer
				io.Copy(&valBuf, part)
				partFolder := strings.TrimSpace(valBuf.String())
				if targetFolder == "" && partFolder != "" {
					targetFolder = partFolder
					if df, safeErr := resolveSafe(targetFolder); safeErr == nil {
						destFolder = df
						os.MkdirAll(destFolder, 0755)

						// If files were written before this folder field arrived, relocate them
						if destFolder != initialDestFolder && len(writtenFiles) > 0 {
							for _, oldPath := range writtenFiles {
								base := filepath.Base(oldPath)
								newPath := filepath.Join(destFolder, base)
								os.Rename(oldPath, newPath)
							}
						}
					}
				}
				continue
			}

			if fileName == "" {
				// Skip non-file form fields
				io.Copy(io.Discard, part)
				continue
			}

			fileName = filepath.Base(fileName)
			if fileName == "." || fileName == "/" || fileName == "\\" || fileName == "" {
				io.Copy(io.Discard, part)
				continue
			}

			destPath := filepath.Join(destFolder, fileName)
			dst, createErr := os.Create(destPath)
			if createErr != nil {
				part.Close()
				http.Error(w, fmt.Sprintf("Failed to create destination file: %v", createErr), http.StatusInternalServerError)
				return
			}

			_, copyErr := io.CopyBuffer(dst, part, buf)
			dst.Close()
			part.Close()

			if copyErr != nil {
				os.Remove(destPath)
				http.Error(w, fmt.Sprintf("Error saving file %s: %v", fileName, copyErr), http.StatusInternalServerError)
				return
			}
			writtenFiles = append(writtenFiles, destPath)
			uploadedCount++
		}

		if uploadedCount == 0 {
			http.Error(w, "No files uploaded", http.StatusBadRequest)
			return
		}
	}

	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" || strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success":true}`))
		return
	}

	if targetFolder != "" {
		http.Redirect(w, r, "/folder/"+url.PathEscape(filepath.ToSlash(targetFolder)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleDownloadFile serves individual files with HTTP range requests (resumeable downloads)
func handleDownloadFile(w http.ResponseWriter, r *http.Request) {
	relPath := r.PathValue("path")
	if relPath == "" {
		// Fallback for older Go or raw URL extraction
		relPath = strings.TrimPrefix(r.URL.Path, "/download/")
	}

	unescaped, err := url.PathUnescape(relPath)
	if err == nil {
		relPath = unescaped
	}

	safePath, err := resolveSafe(relPath)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	info, err := os.Stat(safePath)
	if err != nil || info.IsDir() {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// Set content-disposition so browser triggers a download dialog
	filename := filepath.Base(safePath)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))

	// http.ServeFile handles HTTP range requests, chunking, ETag, and Last-Modified automatically!
	http.ServeFile(w, r, safePath)
}

// handleDownloadFolderZip streams directory contents as a compressed ZIP directly into the HTTP socket.
// It consumes minimal constant RAM even for gigabyte-sized folders!
func handleDownloadFolderZip(w http.ResponseWriter, r *http.Request) {
	folder := r.PathValue("folder")
	if folder == "" {
		folder = strings.TrimPrefix(r.URL.Path, "/download-folder/")
	}

	unescaped, err := url.PathUnescape(folder)
	if err == nil {
		folder = unescaped
	}

	safePath, err := resolveSafe(folder)
	if err != nil {
		http.Error(w, "Folder not found", http.StatusNotFound)
		return
	}

	info, err := os.Stat(safePath)
	if err != nil || !info.IsDir() {
		http.Error(w, "Folder not found", http.StatusNotFound)
		return
	}

	baseName := filepath.Base(safePath)
	if baseName == "." || baseName == "/" || baseName == "\\" {
		baseName = "root_folder"
	}
	safeFilename := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, baseName)

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q.zip", safeFilename))

	zipWriter := zip.NewWriter(w)
	defer zipWriter.Close()

	// Walk through the directory and stream each file into zipWriter
	walkErr := filepath.WalkDir(safePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(safePath, path)
		if err != nil {
			return err
		}

		if rel == "." {
			return nil
		}

		// Use standard forward slashes inside ZIP archives
		zipEntryPath := filepath.ToSlash(filepath.Join(safeFilename, rel))

		info, err := d.Info()
		if err != nil {
			return err
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = zipEntryPath

		if d.IsDir() {
			header.Name += "/"
			_, err = zipWriter.CreateHeader(header)
			return err
		}

		header.Method = zip.Deflate // Use standard Deflate compression

		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			return err
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		// Stream file content directly into zip stream
		_, err = io.Copy(writer, file)
		return err
	})

	if walkErr != nil {
		log.Printf("Error creating zip for %s: %v", safePath, walkErr)
	}
}

// handleDelete removes a file or directory recursively
func handleDelete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	target := r.FormValue("target")
	if strings.TrimSpace(target) == "" {
		http.Error(w, "Target path required", http.StatusBadRequest)
		return
	}

	safePath, err := resolveSafe(target)
	if err != nil {
		http.Error(w, "Invalid target", http.StatusBadRequest)
		return
	}

	if _, err := os.Stat(safePath); os.IsNotExist(err) {
		http.Error(w, "Item not found", http.StatusNotFound)
		return
	}

	// Remove file or recursive directory
	if err := os.RemoveAll(safePath); err != nil {
		http.Error(w, fmt.Sprintf("Delete failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Determine redirect URL to parent folder or root
	absUploads, _ := filepath.Abs(uploadDir)
	rel, err := filepath.Rel(absUploads, safePath)
	if err == nil {
		parent := filepath.Dir(rel)
		if parent != "." && parent != "" && parent != "/" && parent != "\\" {
			http.Redirect(w, r, "/folder/"+url.PathEscape(filepath.ToSlash(parent)), http.StatusSeeOther)
			return
		}
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleMove moves a file or folder into a target directory
func handleMove(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	source := strings.TrimSpace(r.FormValue("source"))
	destFolder := strings.TrimSpace(r.FormValue("destination"))

	if source == "" {
		http.Error(w, "Source required", http.StatusBadRequest)
		return
	}

	safeSource, err := resolveSafe(source)
	if err != nil {
		http.Error(w, "Invalid source path", http.StatusBadRequest)
		return
	}

	srcInfo, err := os.Stat(safeSource)
	if err != nil {
		http.Error(w, "Source item not found", http.StatusNotFound)
		return
	}

	safeDestDir, err := resolveSafe(destFolder)
	if err != nil {
		http.Error(w, "Invalid destination folder", http.StatusBadRequest)
		return
	}

	destInfo, err := os.Stat(safeDestDir)
	if err != nil || !destInfo.IsDir() {
		http.Error(w, "Destination must be an existing directory", http.StatusBadRequest)
		return
	}

	itemName := filepath.Base(safeSource)
	targetPath := filepath.Join(safeDestDir, itemName)

	// If source and target are the same, nothing to do
	if filepath.Clean(safeSource) == filepath.Clean(targetPath) {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Prevent moving a folder into itself or a subfolder of itself
	if srcInfo.IsDir() {
		rel, err := filepath.Rel(safeSource, targetPath)
		if err == nil && !strings.HasPrefix(rel, "..") {
			http.Error(w, "Cannot move a directory into itself or its subdirectory", http.StatusBadRequest)
			return
		}
	}

	// Prevent overwriting existing items
	if _, err := os.Stat(targetPath); err == nil {
		http.Error(w, "An item with the same name already exists in target folder", http.StatusConflict)
		return
	}

	if err := os.Rename(safeSource, targetPath); err != nil {
		http.Error(w, fmt.Sprintf("Move failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
