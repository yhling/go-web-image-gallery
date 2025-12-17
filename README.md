# YAIG - Yet Another Image Gallery

A super fast zero-dependency Go based web server that turns your directories into a image gallery. Making it iCloud-like.

## Features

- Standalone executable. No DB, no frameworks, no containers.
- Supports previewing almost every image format (including HEIC, DNG, ARW). Video playback depends on browser.
- Fast preview and thumbnail generation

### Run prerequisites

**Windows:**
Download and run the .exe

**macOS:**
```bash
brew install vips ffmpeg

```
**Ubuntu/Debian:**
```bash
sudo apt-get install libvips-dev ffmpeg
```

**Fedora:**
```bash
sudo dnf install vips-devel ffmpeg
```

### Build (for Mac/Linux)

```bash
go build -o directory-server
```

## Usage

### Command-Line Arguments

Run the server with command-line arguments:

```bash
./directory-server -root /path/to/your/directory -port 8080
```

**Arguments:**
- `-root`: Root directory to serve (default: current directory)
- `-port`: Port to listen on (default: 8080)

**Examples:**

```bash
# Use default settings (current directory, port 8080)
./directory-server

# Specify root directory
./directory-server -root /path/to/images

# Specify both root directory and port
./directory-server -root /path/to/images -port 3000
```

### Access the Web Interface

Open your browser and navigate to:
```
http://localhost:8080
```

## API Endpoints

- `GET /` - Main web interface
- `GET /api/list?path=/` - JSON API for directory listing
- `GET /api/thumbnail/<path>` - Serves thumbnail for an image
- `GET /static/<path>` - Serves static files

## Security

The server includes path traversal protection to ensure users can only access files within the specified root directory.

