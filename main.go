package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/cshum/vipsgen/vips"
)

type Server struct {
	rootDir   string
	indexTmpl *template.Template
}

type FileInfo struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	IsDir     bool   `json:"isDir"`
	IsImage   bool   `json:"isImage"`
	Thumbnail string `json:"thumbnail,omitempty"`
}

type DirectoryResponse struct {
	Path  string     `json:"path"`
	Files []FileInfo `json:"files"`
}

var imageExtensions = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".heic": true,
	".HEIC": true,
}

func init() {
	vips.Startup(nil)
}

func main() {
	rootDir := os.Getenv("ROOT_DIR")
	if rootDir == "" {
		rootDir = "."
	}

	// Convert to absolute path
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		log.Fatalf("Failed to get absolute path: %v", err)
	}

	// Load template
	tmpl, err := template.ParseFiles("templates/index.html")
	if err != nil {
		log.Fatalf("Failed to load template: %v", err)
	}

	server := &Server{
		rootDir:   absRoot,
		indexTmpl: tmpl,
	}

	http.HandleFunc("/", server.handleIndex)
	http.HandleFunc("/api/list", server.handleList)
	http.HandleFunc("/api/thumbnail/", server.handleThumbnail)
	http.HandleFunc("/api/preview/", server.handlePreview)
	http.HandleFunc("/static/", server.handleStatic)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server starting on port %s, serving directory: %s", port, absRoot)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.indexTmpl.Execute(w, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "/"
	}

	// Clean the path
	path = filepath.Clean(path)
	if path == "." {
		path = "/"
	}

	// Build full path
	fullPath := filepath.Join(s.rootDir, path)
	if path == "/" {
		fullPath = s.rootDir
	}

	// Security check: ensure path is within root directory
	relPath, err := filepath.Rel(s.rootDir, fullPath)
	if err != nil || strings.HasPrefix(relPath, "..") {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Read directory
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		respondJSON(w, map[string]interface{}{
			"error": err.Error(),
		}, http.StatusInternalServerError)
		return
	}

	var files []FileInfo
	for _, entry := range entries {
		// Skip hidden directories like .small
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		entryPath := filepath.Join(fullPath, entry.Name())
		relEntryPath := filepath.Join(path, entry.Name())
		if path == "/" {
			relEntryPath = "/" + entry.Name()
		}
		// Convert to URL path format (forward slashes)
		urlPath := strings.ReplaceAll(relEntryPath, "\\", "/")

		fileInfo := FileInfo{
			Name:  entry.Name(),
			Path:  urlPath,
			IsDir: entry.IsDir(),
		}

		// Check if it's an image
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if imageExtensions[ext] {
			fileInfo.IsImage = true
			// Generate thumbnail path - ensure it starts with / for proper URL
			thumbPath := urlPath
			if !strings.HasPrefix(thumbPath, "/") {
				thumbPath = "/" + thumbPath
			}
			fileInfo.Thumbnail = "/api/thumbnail" + thumbPath
			// Generate thumbnail asynchronously
			go s.generateThumbnail(entryPath)
		}

		files = append(files, fileInfo)
	}

	respondJSON(w, DirectoryResponse{
		Path:  path,
		Files: files,
	}, http.StatusOK)
}

func (s *Server) handleThumbnail(w http.ResponseWriter, r *http.Request) {
	// Extract path from URL - Go's http package already URL decodes the path
	rawPath := strings.TrimPrefix(r.URL.Path, "/api/thumbnail")
	// Remove leading slash
	rawPath = strings.TrimPrefix(rawPath, "/")
	if rawPath == "" {
		http.Error(w, "Path required", http.StatusBadRequest)
		return
	}

	// Convert URL path (forward slashes) to filesystem path
	path := filepath.FromSlash(rawPath)

	// Clean the path
	path = filepath.Clean(path)
	if path == "." {
		path = "/"
	}

	// Build full path
	var fullPath string
	if path == "/" || path == "" {
		fullPath = s.rootDir
	} else {
		fullPath = filepath.Join(s.rootDir, path)
	}

	// Security check
	relPath, err := filepath.Rel(s.rootDir, fullPath)
	if err != nil || strings.HasPrefix(relPath, "..") {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Check if file exists
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// Generate thumbnail path
	dir := filepath.Dir(fullPath)
	baseName := filepath.Base(fullPath)
	ext := filepath.Ext(baseName)
	nameWithoutExt := strings.TrimSuffix(baseName, ext)
	thumbnailDir := filepath.Join(dir, ".small")
	thumbnailPath := filepath.Join(thumbnailDir, nameWithoutExt+".jpg")

	// Check if thumbnail exists
	if _, err := os.Stat(thumbnailPath); os.IsNotExist(err) {
		// Generate thumbnail
		if err := s.generateThumbnail(fullPath); err != nil {
			http.Error(w, "Failed to generate thumbnail: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Serve thumbnail
	http.ServeFile(w, r, thumbnailPath)
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	// Extract path from URL
	rawPath := strings.TrimPrefix(r.URL.Path, "/api/preview")
	// Remove leading slash
	rawPath = strings.TrimPrefix(rawPath, "/")
	if rawPath == "" {
		http.Error(w, "Path required", http.StatusBadRequest)
		return
	}

	// Convert URL path (forward slashes) to filesystem path
	path := filepath.FromSlash(rawPath)

	// Clean the path
	path = filepath.Clean(path)
	if path == "." {
		path = "/"
	}

	// Build full path
	var fullPath string
	if path == "/" || path == "" {
		fullPath = s.rootDir
	} else {
		fullPath = filepath.Join(s.rootDir, path)
	}

	// Security check
	relPath, err := filepath.Rel(s.rootDir, fullPath)
	if err != nil || strings.HasPrefix(relPath, "..") {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Check if file exists
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// Check if it's an image
	ext := strings.ToLower(filepath.Ext(fullPath))
	if !imageExtensions[ext] {
		http.Error(w, "Not an image file", http.StatusBadRequest)
		return
	}

	// Load image (for both JPG and non-JPG, we need to resize)
	img, err := vips.NewImageFromFile(fullPath, nil)
	if err != nil {
		http.Error(w, "Failed to load image: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer img.Close()

	// Resize to maximum 1600px on the longest side while maintaining aspect ratio
	width := img.Width()
	height := img.Height()
	maxSize := 1600

	var scale float64
	if width > height {
		if width > maxSize {
			scale = float64(maxSize) / float64(width)
		} else {
			scale = 1.0
		}
	} else {
		if height > maxSize {
			scale = float64(maxSize) / float64(height)
		} else {
			scale = 1.0
		}
	}

	// Resize image if needed
	if scale < 1.0 {
		if err := img.Resize(scale, nil); err != nil {
			http.Error(w, "Failed to resize image: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Convert to JPEG using temporary file (vipsgen doesn't have direct writer method)
	// Create temp file
	tmpFile, err := os.CreateTemp("", "preview-*.jpg")
	if err != nil {
		http.Error(w, "Failed to create temp file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	// Save as JPEG
	if err := img.Jpegsave(tmpPath, nil); err != nil {
		http.Error(w, "Failed to convert image: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Read the converted file
	jpegData, err := os.ReadFile(tmpPath)
	if err != nil {
		http.Error(w, "Failed to read converted image: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Set content type and headers
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(jpegData)))

	// Write the JPEG data to response
	w.Write(jpegData)
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/static/")
	if path == "" {
		http.Error(w, "Path required", http.StatusBadRequest)
		return
	}

	// Clean the path
	path = filepath.Clean(path)
	if path == "." {
		path = "/"
	}

	// Build full path
	fullPath := filepath.Join(s.rootDir, path)
	if path == "/" {
		fullPath = s.rootDir
	}

	// Security check
	relPath, err := filepath.Rel(s.rootDir, fullPath)
	if err != nil || strings.HasPrefix(relPath, "..") {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Check if file exists
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// Serve file
	http.ServeFile(w, r, fullPath)
}

func (s *Server) generateThumbnail(imagePath string) error {
	// Check if already exists
	dir := filepath.Dir(imagePath)
	baseName := filepath.Base(imagePath)
	ext := filepath.Ext(baseName)
	nameWithoutExt := strings.TrimSuffix(baseName, ext)
	thumbnailDir := filepath.Join(dir, ".small")
	thumbnailPath := filepath.Join(thumbnailDir, nameWithoutExt+".jpg")

	// Check if thumbnail already exists
	if _, err := os.Stat(thumbnailPath); err == nil {
		return nil
	}

	// Create .small directory if it doesn't exist
	if err := os.MkdirAll(thumbnailDir, 0755); err != nil {
		return fmt.Errorf("failed to create thumbnail directory: %w", err)
	}

	// Load image with vips
	img, err := vips.NewImageFromFile(imagePath, nil)
	if err != nil {
		return fmt.Errorf("failed to load image: %w", err)
	}
	defer img.Close()

	// Calculate thumbnail size (max 300x300, maintain aspect ratio)
	width := img.Width()
	height := img.Height()
	maxSize := 300

	var scale float64
	if width > height {
		if width > maxSize {
			scale = float64(maxSize) / float64(width)
		} else {
			scale = 1.0
		}
	} else {
		if height > maxSize {
			scale = float64(maxSize) / float64(height)
		} else {
			scale = 1.0
		}
	}

	// Resize image if needed
	if scale < 1.0 {
		if err := img.Resize(scale, nil); err != nil {
			return fmt.Errorf("failed to resize image: %w", err)
		}
	}

	// Save as JPEG directly
	if err := img.Jpegsave(thumbnailPath, nil); err != nil {
		return fmt.Errorf("failed to save thumbnail: %w", err)
	}

	return nil
}

func respondJSON(w http.ResponseWriter, data interface{}, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}
