# Directory Browser Server

A Go web server that displays directories in a grid layout with automatic thumbnail generation for images using libvips.

## Features

- Web-based directory browser with modern grid layout
- Automatic thumbnail generation for JPG, PNG, and HEIC images
- Thumbnails stored in `.small` subdirectories
- Fast and efficient image processing using libvips
- Configurable root directory

## Prerequisites

1. **Go** (version 1.21 or later)
2. **libvips** - Image processing library

### Installing libvips and pkg-config

**macOS:**
```bash
brew install vips pkg-config
```

**Ubuntu/Debian:**
```bash
sudo apt-get install libvips-dev pkg-config
```

**Fedora:**
```bash
sudo dnf install vips-devel pkg-config
```

## Installation

1. Install Go dependencies:
```bash
go mod download
```

## Usage

### Set Root Directory

Set the `ROOT_DIR` environment variable to specify the root directory to serve:

```bash
export ROOT_DIR=/path/to/your/directory
```

If not set, it defaults to the current directory.

### Set Port (Optional)

By default, the server runs on port 8080. You can change it with the `PORT` environment variable:

```bash
export PORT=3000
```

### Run the Server

```bash
go run main.go
```

Or build and run:

```bash
go build -o directory-server
./directory-server
```

### Access the Web Interface

Open your browser and navigate to:
```
http://localhost:8080
```

## How It Works

1. **Directory Listing**: The server reads directories and displays files in a grid layout
2. **Image Detection**: Automatically detects JPG, PNG, and HEIC files
3. **Thumbnail Generation**: When an image is accessed, a thumbnail is generated using libvips
4. **Thumbnail Storage**: Thumbnails are stored as JPG files in a `.small` subdirectory within each directory containing images
5. **Caching**: Thumbnails are generated once and cached for subsequent requests

## API Endpoints

- `GET /` - Main web interface
- `GET /api/list?path=/` - JSON API for directory listing
- `GET /api/thumbnail/<path>` - Serves thumbnail for an image
- `GET /static/<path>` - Serves static files

## Security

The server includes path traversal protection to ensure users can only access files within the specified root directory.

