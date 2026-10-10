package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFileServer(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "fileserver_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	uploadDir = tempDir

	mux := http.NewServeMux()
	mux.HandleFunc("GET /css/style.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Write(styleCSS)
	})
	mux.HandleFunc("GET /", handleIndex)
	mux.HandleFunc("GET /api/folder", handleAPIFolder)
	mux.HandleFunc("POST /folder", handleCreateFolder)
	mux.HandleFunc("POST /upload", handleUpload)
	mux.HandleFunc("GET /download/{path...}", handleDownloadFile)
	mux.HandleFunc("GET /download-folder/{folder...}", handleDownloadFolderZip)
	mux.HandleFunc("POST /delete", handleDelete)
	mux.HandleFunc("POST /move", handleMove)

	server := httptest.NewServer(mux)
	defer server.Close()

	// 1. Test index page
	t.Run("GET /", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/")
		if err != nil {
			t.Fatalf("Failed to GET /: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200, got %d", resp.StatusCode)
		}
	})

	// 2. Test CSS endpoint
	t.Run("GET /css/style.css", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/css/style.css")
		if err != nil {
			t.Fatalf("Failed to GET CSS: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200, got %d", resp.StatusCode)
		}
		if resp.Header.Get("Content-Type") != "text/css; charset=utf-8" {
			t.Errorf("Unexpected Content-Type: %s", resp.Header.Get("Content-Type"))
		}
	})

	// 3. Test Create Folder
	t.Run("POST /folder", func(t *testing.T) {
		resp, err := http.PostForm(server.URL+"/folder", map[string][]string{
			"name": {"documents"},
		})
		if err != nil {
			t.Fatalf("Failed to create folder: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusSeeOther {
			t.Errorf("Expected 200 or 303, got %d", resp.StatusCode)
		}

		if _, err := os.Stat(filepath.Join(tempDir, "documents")); os.IsNotExist(err) {
			t.Errorf("Folder 'documents' was not created on disk")
		}
	})

	// 4. Test File Upload into subfolder
	t.Run("POST /upload", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		if err := writer.WriteField("folder", "documents"); err != nil {
			t.Fatalf("Failed to write folder field: %v", err)
		}

		part, err := writer.CreateFormFile("file", "test.txt")
		if err != nil {
			t.Fatalf("Failed to create form file: %v", err)
		}
		part.Write([]byte("Hello from Go File Server!"))
		writer.Close()

		req, err := http.NewRequest("POST", server.URL+"/upload", body)
		if err != nil {
			t.Fatalf("Failed to build upload req: %v", err)
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())

		client := &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse // don't follow redirect
			},
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Failed to upload: %v", err)
		}
		defer resp.Body.Close()

		uploadedFile := filepath.Join(tempDir, "documents", "test.txt")
		content, err := os.ReadFile(uploadedFile)
		if err != nil {
			t.Fatalf("Uploaded file missing: %v", err)
		}
		if string(content) != "Hello from Go File Server!" {
			t.Errorf("Unexpected file content: %s", string(content))
		}
	})

	// 4b. Test File Upload where 'file' is placed BEFORE 'folder' in multipart payload
	t.Run("POST /upload file before folder field", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		part, err := writer.CreateFormFile("file", "second.txt")
		if err != nil {
			t.Fatalf("Failed to create form file: %v", err)
		}
		part.Write([]byte("Second file content"))

		if err := writer.WriteField("folder", "documents"); err != nil {
			t.Fatalf("Failed to write folder field: %v", err)
		}
		writer.Close()

		req, err := http.NewRequest("POST", server.URL+"/upload", body)
		if err != nil {
			t.Fatalf("Failed to build upload req: %v", err)
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())

		client := &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Failed to upload: %v", err)
		}
		defer resp.Body.Close()

		uploadedFile := filepath.Join(tempDir, "documents", "second.txt")
		content, err := os.ReadFile(uploadedFile)
		if err != nil {
			t.Fatalf("Uploaded file missing in documents: %v", err)
		}
		if string(content) != "Second file content" {
			t.Errorf("Unexpected file content: %s", string(content))
		}
	})

	// 5. Test API Folder list
	t.Run("GET /api/folder?path=documents", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/folder?path=documents")
		if err != nil {
			t.Fatalf("Failed to get folder API: %v", err)
		}
		defer resp.Body.Close()

		var result APIResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("Failed to decode JSON: %v", err)
		}

		if len(result.Items) != 2 {
			t.Errorf("Expected 2 items in documents folder, got %+v", result.Items)
		}
	})

	// 6. Test File Download
	t.Run("GET /download/documents/test.txt", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/download/documents/test.txt")
		if err != nil {
			t.Fatalf("Failed to download file: %v", err)
		}
		defer resp.Body.Close()

		bodyBytes, _ := io.ReadAll(resp.Body)
		if string(bodyBytes) != "Hello from Go File Server!" {
			t.Errorf("Downloaded content mismatch: %s", string(bodyBytes))
		}
	})

	// 7. Test Streaming ZIP download of folder
	t.Run("GET /download-folder/documents", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/download-folder/documents")
		if err != nil {
			t.Fatalf("Failed to download folder zip: %v", err)
		}
		defer resp.Body.Close()

		zipBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read zip: %v", err)
		}

		zipReader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
		if err != nil {
			t.Fatalf("Invalid zip response: %v", err)
		}

		found := false
		for _, f := range zipReader.File {
			if filepath.Base(f.Name) == "test.txt" {
				found = true
				rc, _ := f.Open()
				b, _ := io.ReadAll(rc)
				rc.Close()
				if string(b) != "Hello from Go File Server!" {
					t.Errorf("Zip file content mismatch: %s", string(b))
				}
			}
		}
		if !found {
			t.Errorf("test.txt was not found inside the generated zip")
		}
	})

	// 8. Test Delete
	t.Run("POST /delete", func(t *testing.T) {
		client := &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		resp, err := client.PostForm(server.URL+"/delete", map[string][]string{
			"target": {"documents/test.txt"},
		})
		if err != nil {
			t.Fatalf("Failed to delete: %v", err)
		}
		defer resp.Body.Close()

		if _, err := os.Stat(filepath.Join(tempDir, "documents", "test.txt")); !os.IsNotExist(err) {
			t.Errorf("File was not deleted")
		}
	})

	// 9. Test Move
	t.Run("POST /move", func(t *testing.T) {
		// Create a root file
		rootFile := filepath.Join(tempDir, "root_sample.txt")
		if err := os.WriteFile(rootFile, []byte("Move me!"), 0644); err != nil {
			t.Fatalf("Failed to create sample file: %v", err)
		}

		resp, err := http.PostForm(server.URL+"/move", map[string][]string{
			"source":      {"root_sample.txt"},
			"destination": {"documents"},
		})
		if err != nil {
			t.Fatalf("Failed to post move: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d", resp.StatusCode)
		}

		// Verify old file is gone and new file exists in destination
		if _, err := os.Stat(rootFile); !os.IsNotExist(err) {
			t.Errorf("Source file still exists after move")
		}
		movedFile := filepath.Join(tempDir, "documents", "root_sample.txt")
		content, err := os.ReadFile(movedFile)
		if err != nil {
			t.Fatalf("Moved file does not exist in target: %v", err)
		}
		if string(content) != "Move me!" {
			t.Errorf("Unexpected content: %s", string(content))
		}
	})

	// 10. Security: Verify path traversal attacks are rejected
	t.Run("Security: Path Traversal Rejections", func(t *testing.T) {
		maliciousVectors := []string{
			"../",
			"../../",
			"../../main.go",
			"..\\..\\main.go",
			"/../main.go",
			"\\..\\main.go",
			"C:/Windows/System32",
			"C:Windows",
			"D:/sensitive.txt",
			"\\\\localhost\\c$\\secret.txt",
			"documents/../../../../../../Windows",
			"documents/..//..//main.go",
			"./../main.go",
			"foo/bar/../../../baz",
		}

		for _, path := range maliciousVectors {
			resolved, err := resolveSafe(path)
			if err == nil {
				t.Errorf("Security flaw! Malicious path %q was accepted and resolved to %q", path, resolved)
			}
		}
	})
}

