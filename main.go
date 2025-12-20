package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Server struct {
	rootDir             string
	basePath            string
	indexTmpl           *template.Template
	imageThumbnailQueue chan string
	movieThumbnailQueue chan string
	imageWorkersWg      sync.WaitGroup
	movieWorkersWg      sync.WaitGroup
	pendingThumbs       sync.Map // map[string]chan struct{} - tracks pending thumbnail generations
}

type FileInfo struct {
	Name           string `json:"name"`
	Path           string `json:"path"`
	IsDir          bool   `json:"isDir"`
	IsImage        bool   `json:"isImage"`
	IsMovie        bool   `json:"isMovie"`
	Thumbnail      string `json:"thumbnail,omitempty"`
	CanonicalMovie string `json:"canonicalMovie,omitempty"`
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
	".arw":  true,
	".ARW":  true,
	".raw":  true,
	".RAW":  true,
	".heif": true,
	".HEIF": true,
	".dng":  true,
	".DNG":  true,
}

var movieExtensions = map[string]bool{
	".mov": true,
	".MOV": true,
	".mp4": true,
	".MP4": true,
	".avi": true,
	".AVI": true,
	".mkv": true,
	".MKV": true,
}

// vipsExecutable returns the path to the vips executable
// On Windows, it looks for vipsthumbnail.exe, otherwise just "vipsthumbnail"
func vipsExecutable() string {
	if _, err := exec.LookPath("vipsthumbnail.exe"); err == nil {
		return "vipsthumbnail.exe"
	}
	return "vipsthumbnail"
}

// urlWithBasePath prepends the base path to a URL path
func (s *Server) urlWithBasePath(path string) string {
	if s.basePath == "" {
		return path
	}
	// Ensure path starts with /
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return s.basePath + path
}

// getThumbnailPath returns the thumbnail path for a given image path
// The thumbnail filename includes the original extension to avoid conflicts
// between files with the same base name but different extensions
func getThumbnailPath(imagePath string) string {
	dir := filepath.Dir(imagePath)
	baseName := filepath.Base(imagePath)
	// Include the original extension in the thumbnail filename
	// e.g., photo.jpg -> photo.jpg.jpg, photo.png -> photo.png.jpg
	thumbnailDir := filepath.Join(dir, ".small")
	thumbnailPath := filepath.Join(thumbnailDir, baseName+".jpg")
	return thumbnailPath
}

func main() {
	// Parse command-line arguments
	rootDir := flag.String("root", ".", "Root directory to serve (default: current directory)")
	port := flag.String("port", "8080", "Port to listen on (default: 8080)")
	basePath := flag.String("base-path", "", "Base path for the application (e.g., /gallery)")
	flag.Parse()

	// On Windows, add ./bin to PATH
	if runtime.GOOS == "windows" {
		binPath, err := filepath.Abs("./bin")
		if err == nil {
			currentPath := os.Getenv("PATH")
			// Prepend binPath to PATH if it's not already there
			if !strings.Contains(currentPath, binPath) {
				newPath := binPath + string(filepath.ListSeparator) + currentPath
				os.Setenv("PATH", newPath)
			}
		}
	}

	// Convert to absolute path
	absRoot, err := filepath.Abs(*rootDir)
	if err != nil {
		log.Fatalf("Failed to get absolute path: %v", err)
	}

	// Load template
	tmpl, err := template.ParseFiles("templates/index.html")
	if err != nil {
		log.Fatalf("Failed to load template: %v", err)
	}

	// Initialize thumbnail queues with buffer to prevent blocking
	// Buffer size of 500 allows some queuing before blocking
	queueSize := 250
	numImageWorkers := 2 // Limit concurrent image thumbnail generations to prevent memory issues
	numMovieWorkers := 1 // Limit concurrent movie thumbnail generations (movies are more resource-intensive)

	// Normalize base path: ensure it starts with / and ends without /
	normalizedBasePath := *basePath
	if normalizedBasePath != "" {
		if !strings.HasPrefix(normalizedBasePath, "/") {
			normalizedBasePath = "/" + normalizedBasePath
		}
		normalizedBasePath = strings.TrimSuffix(normalizedBasePath, "/")
	}

	server := &Server{
		rootDir:             absRoot,
		basePath:            normalizedBasePath,
		indexTmpl:           tmpl,
		imageThumbnailQueue: make(chan string, queueSize),
		movieThumbnailQueue: make(chan string, queueSize),
	}

	// Start image worker goroutines
	for i := 0; i < numImageWorkers; i++ {
		server.imageWorkersWg.Add(1)
		go server.imageThumbnailWorker(i)
	}

	// Start movie worker goroutines
	for i := 0; i < numMovieWorkers; i++ {
		server.movieWorkersWg.Add(1)
		go server.movieThumbnailWorker(i)
	}

	http.HandleFunc("/", server.handleIndex)
	http.HandleFunc("/api/list", server.handleList)
	http.HandleFunc("/api/thumbnail/", server.handleThumbnail)
	http.HandleFunc("/api/preview/", server.handlePreview)
	http.HandleFunc("/static/", server.handleStatic)

	log.Printf("Server starting on port %s, serving directory: %s", *port, absRoot)
	log.Fatal(http.ListenAndServe(":"+*port, nil))
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templateData := map[string]string{
		"BasePath": s.basePath,
	}
	if err := s.indexTmpl.Execute(w, templateData); err != nil {
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
		if imageExtensions[ext] || movieExtensions[ext] {
			if imageExtensions[ext] {
				fileInfo.IsImage = true
			}
			if movieExtensions[ext] {
				fileInfo.IsMovie = true
			}
			// Generate thumbnail path - ensure it starts with / for proper URL
			thumbPath := urlPath
			if !strings.HasPrefix(thumbPath, "/") {
				thumbPath = "/" + thumbPath
			}
			fileInfo.Thumbnail = s.urlWithBasePath("/api/thumbnail" + thumbPath)
			// Thumbnail will be generated on-demand when client requests it
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
	thumbnailPath := getThumbnailPath(fullPath)

	// Check if thumbnail exists
	if _, err := os.Stat(thumbnailPath); os.IsNotExist(err) {
		// Queue thumbnail generation and wait for it to complete
		if err := s.queueAndWaitForThumbnail(fullPath, thumbnailPath); err != nil {
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

	// Check if it's an image or movie
	ext := strings.ToLower(filepath.Ext(fullPath))
	isImage := imageExtensions[ext]
	isMovie := movieExtensions[ext]

	if !isImage && !isMovie {
		http.Error(w, "Not an image or movie file", http.StatusBadRequest)
		return
	}

	// Set cache control header
	w.Header().Set("Cache-Control", "public, max-age=3600")

	if isMovie {
		// Handle movie files with ffmpeg
		// Use ffmpeg to transcode: hevc_qsv input -> h264_qsv output, streaming to HTTP response
		w.Header().Set("Content-Type", "video/mp2t")

		cmd := exec.Command("ffmpeg",
			"-c:v", "hevc_qsv",
			"-loglevel", "quiet",
			"-i", fullPath,
			"-c:a", "aac",
			"-b:a", "64k",
			"-c:v", "h264_qsv",
			"-b:v", "500k",
			"-f", "mpegts",
			"pipe:1")
		cmd.Stderr = os.Stderr
		cmd.Stdout = w // Output to HTTP response

		// Execute command and stream output directly to response
		if err := cmd.Run(); err != nil {
			// If we've already started writing, we can't send an error response
			log.Printf("Failed to process movie %s: %v", fullPath, err)
			return
		}
	} else {
		// Handle image files with vips
		// Use vips to resize and convert to JPEG, streaming directly to HTTP response
		// This avoids creating any temporary files - streams directly from vips to client
		vipsCmd := vipsExecutable()

		// Set content type and headers before writing
		w.Header().Set("Content-Type", "image/jpeg")

		// Use vips thumbnail reading input from stdin
		// Open the file for reading
		file, err := os.Open(fullPath)
		if err != nil {
			http.Error(w, "Failed to open file", http.StatusInternalServerError)
			return
		}
		defer file.Close()

		// Use "-" for stdin and stdout
		cmd := exec.Command(vipsCmd, "stdin", "-s", "1600", "-o", ".jpg")
		cmd.Stderr = os.Stderr
		cmd.Stdout = w   // Output to HTTP response
		cmd.Stdin = file // Input comes from file

		// Execute command and stream output directly to response
		if err := cmd.Run(); err != nil {
			// If we've already started writing, we can't send an error response
			log.Printf("Failed to process image %s: %v", fullPath, err)
			return
		}
	}
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
	// Get thumbnail path (includes original extension)
	thumbnailPath := getThumbnailPath(imagePath)
	thumbnailDir := filepath.Dir(thumbnailPath)

	// Check if thumbnail already exists
	if _, err := os.Stat(thumbnailPath); err == nil {
		return nil
	}

	// Create .small directory if it doesn't exist
	if err := os.MkdirAll(thumbnailDir, 0755); err != nil {
		return fmt.Errorf("failed to create thumbnail directory: %w", err)
	}

	// Check file extension to determine if it's a movie or image
	ext := strings.ToLower(filepath.Ext(imagePath))

	if movieExtensions[ext] {
		// Use ffmpeg for movie files, print only errors
		// ffmpeg -v error -i <input> -ss 1 -vf "scale=300:-2" -vframes 1 <out>
		cmd := exec.Command("ffmpeg", "-v", "error", "-ss", "0", "-noaccurate_seek", "-i", imagePath, "-vf", "scale=300:-2", "-vframes", "1", thumbnailPath)
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to generate thumbnail: %w", err)
		}
	} else if imageExtensions[ext] {
		// Use vips to read from stdin and output a .jpg, resize to 1600px
		vipsCmd := vipsExecutable()
		file, err := os.Open(imagePath)
		if err != nil {
			return fmt.Errorf("failed to open image for vips stdin: %w", err)
		}
		defer file.Close()

		cmd := exec.Command(vipsCmd, "stdin", "-s", "300", "-o", thumbnailPath)
		cmd.Stdin = file
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to generate thumbnail: %w", err)
		}
	} else {
		return fmt.Errorf("unsupported file type for thumbnail generation")
	}

	return nil
}

func (s *Server) queueAndWaitForThumbnail(imagePath, thumbnailPath string) error {
	// Check if thumbnail is already being generated
	doneChan, alreadyGenerating := s.pendingThumbs.LoadOrStore(thumbnailPath, make(chan struct{}))
	done := doneChan.(chan struct{})

	if !alreadyGenerating {
		// Determine file type to route to appropriate queue
		ext := strings.ToLower(filepath.Ext(imagePath))
		var targetQueue chan string

		if movieExtensions[ext] {
			targetQueue = s.movieThumbnailQueue
		} else if imageExtensions[ext] {
			targetQueue = s.imageThumbnailQueue
		} else {
			return fmt.Errorf("unsupported file type for thumbnail generation")
		}

		// We're the first to request this thumbnail, queue it
		select {
		case targetQueue <- imagePath:
			// Successfully queued, wait for completion
		default:
			// Queue is full, generate synchronously as fallback
			err := s.generateThumbnail(imagePath)
			close(done)
			s.pendingThumbs.Delete(thumbnailPath)
			return err
		}
	}

	// Wait for thumbnail generation to complete (with timeout)
	select {
	case <-done:
		// Check if thumbnail was actually created
		if _, err := os.Stat(thumbnailPath); os.IsNotExist(err) {
			return fmt.Errorf("thumbnail generation completed but file not found")
		}
		return nil
	case <-time.After(30 * time.Second):
		return fmt.Errorf("thumbnail generation timeout")
	}
}

func (s *Server) imageThumbnailWorker(workerID int) {
	defer s.imageWorkersWg.Done()

	for imagePath := range s.imageThumbnailQueue {
		// Get thumbnail path to use as key (includes original extension)
		thumbnailPath := getThumbnailPath(imagePath)

		// Generate thumbnail
		err := s.generateThumbnail(imagePath)

		// Notify waiting goroutines that generation is complete
		if doneChan, ok := s.pendingThumbs.LoadAndDelete(thumbnailPath); ok {
			close(doneChan.(chan struct{}))
		}

		if err != nil {
			log.Printf("Image Worker %d: Failed to generate thumbnail for %s: %v", workerID, imagePath, err)
		}
	}
}

func (s *Server) movieThumbnailWorker(workerID int) {
	defer s.movieWorkersWg.Done()

	for moviePath := range s.movieThumbnailQueue {
		// Get thumbnail path to use as key (includes original extension)
		thumbnailPath := getThumbnailPath(moviePath)

		// Generate thumbnail
		err := s.generateThumbnail(moviePath)

		// Notify waiting goroutines that generation is complete
		if doneChan, ok := s.pendingThumbs.LoadAndDelete(thumbnailPath); ok {
			close(doneChan.(chan struct{}))
		}

		if err != nil {
			log.Printf("Movie Worker %d: Failed to generate thumbnail for %s: %v", workerID, moviePath, err)
		}
	}
}

func respondJSON(w http.ResponseWriter, data interface{}, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}
